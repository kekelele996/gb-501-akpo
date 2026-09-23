//go:build integration

// 该文件使用嵌入式 PostgreSQL 验证迁移、部分唯一索引和并发提交。
// 默认 `go test ./...` 不运行（无需下载数据库二进制）；执行：
//
//	go test -tags=integration ./internal/service/ -count=1
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"gorm.io/gorm"

	"sterile-packaging-release-control/backend/internal/constants"
	"sterile-packaging-release-control/backend/internal/dto"
	"sterile-packaging-release-control/backend/internal/model"
	"sterile-packaging-release-control/backend/internal/repository"
	"sterile-packaging-release-control/backend/internal/util"
)

var (
	sharedPG    *embeddedpostgres.EmbeddedPostgres
	sharedDB    *gorm.DB
	setupDBOnce sync.Once
	setupDBErr  error
)

func startEmbeddedPostgres(t *testing.T) *gorm.DB {
	t.Helper()
	setupDBOnce.Do(func() {
		cacheDir := "/tmp/embedded-postgres-bin"
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			setupDBErr = err
			return
		}
		sharedPG = embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
			Port(54399).
			Version(embeddedpostgres.V16).
			CachePath(cacheDir).
			RuntimePath("/tmp/embedded-postgres-run").
			Database("sterile_test").
			Username("postgres").
			Password("postgres"))
		dsn := "postgres://postgres:postgres@localhost:54399/sterile_test?sslmode=disable"
		if err := sharedPG.Start(); err != nil {
			// 上一轮测试二进制退出后嵌入式 Postgres 可能仍在运行，直接复用。
			sharedPG = nil
			if reused, openErr := util.OpenDatabase(dsn); openErr != nil {
				setupDBErr = fmt.Errorf("start embedded postgres: %w", err)
				return
			} else {
				sharedDB = reused
			}
		} else {
			sharedDB, setupDBErr = util.OpenDatabase(dsn)
			if setupDBErr != nil {
				return
			}
		}
		setupDBErr = util.Migrate(sharedDB)
	})
	if setupDBErr != nil {
		t.Fatalf("embedded postgres unavailable: %v", setupDBErr)
	}
	return sharedDB
}

// truncateAll 让每个用例在空库上运行，避免自增序列和跨用例数据互相干扰。
func truncateAll(t *testing.T, db *gorm.DB) {
	t.Helper()
	err := db.Exec(`TRUNCATE TABLE audit_logs, release_decisions, inspection_samples, production_batches, packaging_lines, users RESTART IDENTITY CASCADE`).Error
	if err != nil {
		t.Fatalf("truncate tables: %v", err)
	}
}

type segmentFixture struct {
	db         *gorm.DB
	batchID    uint
	inspection InspectionService
	release    ReleaseService
}

func newSegmentFixture(t *testing.T) segmentFixture {
	db := startEmbeddedPostgres(t)
	truncateAll(t, db)
	line := &model.PackagingLine{
		Code: fmt.Sprintf("L-%d", time.Now().UnixNano()%1_000_000), Name: "集成测试线", Team: "测试班",
		EquipmentStatus: "running", Active: true,
	}
	line.Normalize()
	if err := db.Create(line).Error; err != nil {
		t.Fatalf("create line: %v", err)
	}
	batch := &model.ProductionBatch{
		BatchNo: fmt.Sprintf("B-%d", time.Now().UnixNano()%1_000_000), Specification: "集成测试无菌袋",
		Status: constants.BatchStatusRunning, ResponsibleTeam: "测试班", PackagingLineID: line.ID,
		PlannedQuantity: 1000, ProducedQuantity: 500,
	}
	batch.Normalize()
	if err := db.Create(batch).Error; err != nil {
		t.Fatalf("create batch: %v", err)
	}
	batchRepo := repository.NewBatchRepository(db)
	inspectionRepo := repository.NewInspectionRepository(db)
	releaseRepo := repository.NewReleaseRepository(db)
	audit := NewAuditService(repository.NewAuditRepository(db))
	tx := repository.NewTransactor(db)
	return segmentFixture{
		db:         db,
		batchID:    batch.ID,
		inspection: NewInspectionService(inspectionRepo, batchRepo, audit, tx),
		release:    NewReleaseService(releaseRepo, batchRepo, inspectionRepo, audit, tx),
	}
}

func (f segmentFixture) actor() Actor {
	return Actor{ID: 1, Name: "集成测试员", RequestID: "req-segment-test"}
}

func (f segmentFixture) createSampleRequest(code string, segment constants.SampleSegment) dto.CreateInspectionRequest {
	return dto.CreateInspectionRequest{
		ProductionBatchID: f.batchID,
		SampleCode:        code,
		Segment:           segment,
		InspectionItem:    "热封强度",
		AcceptanceRange:   ">= 1.5 N/15mm",
	}
}

func (f segmentFixture) mustCreate(t *testing.T, code string, segment constants.SampleSegment) *model.InspectionSample {
	t.Helper()
	sample, err := f.inspection.Create(context.Background(), f.actor(), f.createSampleRequest(code, segment))
	if err != nil {
		t.Fatalf("create %s sample %s: %v", segment, code, err)
	}
	return sample
}

func (f segmentFixture) complete(t *testing.T, sampleID uint, result string, requestRetest bool) {
	t.Helper()
	_, err := f.inspection.Complete(context.Background(), f.actor(), sampleID, dto.CompleteInspectionRequest{
		Result: result, MeasuredValue: "1.7 N/15mm", RequestRetest: requestRetest,
	})
	if err != nil {
		t.Fatalf("complete sample %d as %s: %v", sampleID, result, err)
	}
}

func (f segmentFixture) decide(t *testing.T, decision constants.DecisionType) error {
	_, err := f.release.Decide(context.Background(), f.actor(), dto.CreateReleaseDecisionRequest{
		ProductionBatchID: f.batchID,
		Decision:          decision,
		Reason:            "集成测试审批理由不少于五个字",
	})
	return err
}

func (f segmentFixture) batch(t *testing.T) *model.ProductionBatch {
	t.Helper()
	batch, err := repository.NewBatchRepository(f.db).Find(context.Background(), f.batchID)
	if err != nil {
		t.Fatalf("reload batch: %v", err)
	}
	return batch
}

func conflictStatus(err error) int {
	var apiErr *util.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status
	}
	return 0
}

func TestCreateRejectsDuplicateSegmentAndWholeRequest(t *testing.T) {
	f := newSegmentFixture(t)
	f.mustCreate(t, "S-DUP-START-1", constants.SegmentStart)

	// 同批次同一段位再来一条：整次拒绝（409），库里仍只有一条起始段样本。
	_, err := f.inspection.Create(context.Background(), f.actor(), f.createSampleRequest("S-DUP-START-2", constants.SegmentStart))
	if conflictStatus(err) != 409 {
		t.Fatalf("duplicate segment should be a 409 conflict, got %v", err)
	}
	batch := f.batch(t)
	if len(batch.Inspections) != 1 {
		t.Fatalf("rejected request must not leave partial data, found %d samples", len(batch.Inspections))
	}

	// 中段、末段仍可登记。
	f.mustCreate(t, "S-DUP-MIDDLE-1", constants.SegmentMiddle)
	f.mustCreate(t, "S-DUP-END-1", constants.SegmentEnd)
	batch = f.batch(t)
	if len(batch.Inspections) != 3 {
		t.Fatalf("expected 3 samples after one per segment, got %d", len(batch.Inspections))
	}
}

func TestConcurrentCreateSameSegmentOnlyOneWins(t *testing.T) {
	f := newSegmentFixture(t)
	const goroutines = 8
	var wg sync.WaitGroup
	statuses := make(chan int, goroutines)
	start := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, err := f.inspection.Create(context.Background(), f.actor(), f.createSampleRequest(
				fmt.Sprintf("S-RACE-%d", i), constants.SegmentMiddle))
			statuses <- conflictStatus(err)
		}(i)
	}
	close(start)
	wg.Wait()
	close(statuses)
	success, conflict := 0, 0
	for status := range statuses {
		switch status {
		case 0:
			success++
		case 409:
			conflict++
		default:
			t.Fatalf("unexpected status %d", status)
		}
	}
	if success != 1 || conflict != goroutines-1 {
		t.Fatalf("want exactly 1 success and %d conflicts, got %d success / %d conflict", goroutines-1, success, conflict)
	}
	if count := len(f.batch(t).Inspections); count != 1 {
		t.Fatalf("concurrent registrations must serialize to one active sample, got %d", count)
	}
}

func TestFailedSampleFreesSegmentForReregistration(t *testing.T) {
	f := newSegmentFixture(t)
	first := f.mustCreate(t, "S-FAIL-START", constants.SegmentStart)
	f.complete(t, first.ID, "fail", true)

	// 不合格（待复测）样本不再占用段位，允许重新登记同段样本。
	replacement := f.mustCreate(t, "S-PASS-START", constants.SegmentStart)
	f.complete(t, replacement.ID, "pass", false)
	batch := f.batch(t)
	if len(batch.Inspections) != 2 {
		t.Fatalf("failed and replacement samples should coexist, got %d", len(batch.Inspections))
	}

	// 新样本合格后重新占段，第三次登记同段必须拒绝。
	if _, err := f.inspection.Create(context.Background(), f.actor(), f.createSampleRequest("S-DENY-START", constants.SegmentStart)); conflictStatus(err) != 409 {
		t.Fatalf("occupied segment after passing replacement should 409, got %v", err)
	}

	// 旧样本（不合格待复测）复测改判合格会与新合格样本争抢同一名额，必须友好拒绝。
	_, err := f.inspection.Complete(context.Background(), f.actor(), first.ID, dto.CompleteInspectionRequest{
		Result: "pass", MeasuredValue: "1.9 N/15mm",
	})
	if conflictStatus(err) != 409 {
		t.Fatalf("old failed sample cannot pass retest while replacement occupies the segment, got %v", err)
	}
}

func TestReleaseRequiresThreeSegmentCoverage(t *testing.T) {
	f := newSegmentFixture(t)

	// 没有任何检验：放行 409，缺三段。
	err := f.decide(t, constants.DecisionRelease)
	if conflictStatus(err) != 409 || !stringContainsAll(err.Error(), "起始段", "中段", "末段") {
		t.Fatalf("release without inspections must list all missing segments, got %v", err)
	}

	start := f.mustCreate(t, "S-REL-START", constants.SegmentStart)
	f.complete(t, start.ID, "pass", false)
	if err := f.decide(t, constants.DecisionRelease); conflictStatus(err) != 409 || !stringContainsAll(err.Error(), "中段", "末段") {
		t.Fatalf("release must list missing middle/end segments, got %v", err)
	}

	middle := f.mustCreate(t, "S-REL-MIDDLE", constants.SegmentMiddle)
	f.complete(t, middle.ID, "pass", false)
	if err := f.decide(t, constants.DecisionRelease); conflictStatus(err) != 409 || !stringContainsAll(err.Error(), "末段") {
		t.Fatalf("release must list missing end segment, got %v", err)
	}

	// 末段先不合格：仍缺合格覆盖。
	failedEnd := f.mustCreate(t, "S-REL-END-FAIL", constants.SegmentEnd)
	f.complete(t, failedEnd.ID, "fail", true)
	if err := f.decide(t, constants.DecisionRelease); conflictStatus(err) != 409 || !stringContainsAll(err.Error(), "末段") {
		t.Fatalf("failed end sample cannot cover the segment, got %v", err)
	}

	// 待复测也独立阻断（即使三段都有合格样本的场景见下个用例）。
	// 重新登记末段并合格后放行成功。
	passedEnd := f.mustCreate(t, "S-REL-END-PASS", constants.SegmentEnd)
	f.complete(t, passedEnd.ID, "pass", false)
	if err := f.decide(t, constants.DecisionRelease); conflictStatus(err) != 409 || !stringContainsAll(err.Error(), "待复测") {
		t.Fatalf("requested retest must block release independently, got %v", err)
	}
}

func TestQuarantineAndReworkBypassCoverageButNeedReason(t *testing.T) {
	f := newSegmentFixture(t)
	// 没有任何检验、三段全缺：隔离和返工不受限。
	if err := f.decide(t, constants.DecisionQuarantine); err != nil {
		t.Fatalf("quarantine should not require segment coverage: %v", err)
	}

	// 隔离后批次变为 hold，需要再开一个新批次验证返工。
	f2 := newSegmentFixture(t)
	if err := f2.decide(t, constants.DecisionRework); err != nil {
		t.Fatalf("rework should not require segment coverage: %v", err)
	}

	// 理由为空白时必须拒绝：服务端归一化后模型校验返回 400。
	f3 := newSegmentFixture(t)
	_, err := f3.release.Decide(context.Background(), f3.actor(), dto.CreateReleaseDecisionRequest{
		ProductionBatchID: f3.batchID, Decision: constants.DecisionQuarantine, Reason: "  ",
	})
	var apiErr *util.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 400 {
		t.Fatalf("quarantine without reason must be rejected with 400, got %v", err)
	}
}

func TestFullThreeSegmentPassReleaseSucceeds(t *testing.T) {
	f := newSegmentFixture(t)
	for _, segment := range constants.AllSegments() {
		sample := f.mustCreate(t, fmt.Sprintf("S-FULL-%s", segment), segment)
		f.complete(t, sample.ID, "pass", false)
	}
	if err := f.decide(t, constants.DecisionRelease); err != nil {
		t.Fatalf("release with all three passed segments should succeed: %v", err)
	}
	batch := f.batch(t)
	if batch.Status != constants.BatchStatusReleased {
		t.Fatalf("batch status = %s, want released", batch.Status)
	}
}

func TestLegacySamplesAreRecognizedBySamplingPosition(t *testing.T) {
	f := newSegmentFixture(t)
	// 模拟段位功能上线前的历史库：先撤掉索引，插入 segment 为空的历史合格样本，
	// 再重跑迁移（回填段位 -> 重建索引），验证真实升级路径幂等且能识别旧样本。
	if err := f.db.Exec("DROP INDEX IF EXISTS idx_inspection_active_segment").Error; err != nil {
		t.Fatalf("drop active segment index: %v", err)
	}
	now := time.Now()
	legacy := []model.InspectionSample{
		{ProductionBatchID: f.batchID, SampleCode: "L-START", SamplingPosition: "批次起始段", InspectionItem: "热封强度", AcceptanceRange: ">= 1.5", Result: "pass", MeasuredValue: "1.8", RetestStatus: "none", InspectedAt: &now},
		{ProductionBatchID: f.batchID, SampleCode: "L-MID", SamplingPosition: "产线中部", InspectionItem: "热封强度", AcceptanceRange: ">= 1.5", Result: "pass", MeasuredValue: "1.8", RetestStatus: "none", InspectedAt: &now},
		{ProductionBatchID: f.batchID, SampleCode: "L-END", SamplingPosition: "线尾抽样", InspectionItem: "热封强度", AcceptanceRange: ">= 1.5", Result: "pass", MeasuredValue: "1.8", RetestStatus: "none", InspectedAt: &now},
	}
	if err := f.db.Create(&legacy).Error; err != nil {
		t.Fatalf("insert legacy samples: %v", err)
	}
	if err := util.Migrate(f.db); err != nil {
		t.Fatalf("re-run migration over legacy data: %v", err)
	}
	batch := f.batch(t)
	if len(batch.Inspections) != 3 {
		t.Fatalf("expected 3 legacy samples, got %d", len(batch.Inspections))
	}
	wantSegments := map[string]constants.SampleSegment{
		"L-START": constants.SegmentStart, "L-MID": constants.SegmentMiddle, "L-END": constants.SegmentEnd,
	}
	for _, sample := range batch.Inspections {
		if sample.Segment != wantSegments[sample.SampleCode] {
			t.Fatalf("legacy sample %s recognized as %q, want %q", sample.SampleCode, sample.Segment, wantSegments[sample.SampleCode])
		}
	}
	// 旧样本三段齐全且合格，应可直接放行。
	if err := f.decide(t, constants.DecisionRelease); err != nil {
		t.Fatalf("legacy samples covering all segments should release: %v", err)
	}
}

func stringContainsAll(text string, tokens ...string) bool {
	for _, token := range tokens {
		if !contains(text, token) {
			return false
		}
	}
	return true
}

func contains(text, token string) bool {
	for i := 0; i+len(token) <= len(text); i++ {
		if text[i:i+len(token)] == token {
			return true
		}
	}
	return false
}
