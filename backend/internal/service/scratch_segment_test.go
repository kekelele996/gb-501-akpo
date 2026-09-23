//go:build cgo

package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"sterile-packaging-release-control/backend/internal/constants"
	"sterile-packaging-release-control/backend/internal/dto"
	"sterile-packaging-release-control/backend/internal/model"
	"sterile-packaging-release-control/backend/internal/repository"
)

type noopAudit struct{}

func (noopAudit) Record(context.Context, Actor, string, string, uint, any, any) error { return nil }
func (noopAudit) List(context.Context, repository.AuditFilter) (dto.PageResult[model.AuditLog], error) {
	return dto.PageResult[model.AuditLog]{}, nil
}

func newScratchStack(t *testing.T) (*gorm.DB, InspectionService, ReleaseService, *model.ProductionBatch) {
	t.Helper()
	dsn := "file:" + strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()) + "?mode=memory&cache=shared&_pragma=busy_timeout(5000)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.PackagingLine{}, &model.ProductionBatch{}, &model.InspectionSample{}, &model.ReleaseDecision{}, &model.AuditLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxIdleConns(10)
	line := model.PackagingLine{Code: "L-001", Name: "线", Team: "甲班", EquipmentStatus: "running", Location: "A", Active: true}
	if err := db.Create(&line).Error; err != nil {
		t.Fatalf("create line: %v", err)
	}
	batch := &model.ProductionBatch{
		BatchNo: "B-001", Specification: "无菌袋", Status: constants.BatchStatusRunning,
		ResponsibleTeam: "甲班", PackagingLineID: line.ID, PlannedQuantity: 100, ProducedQuantity: 100,
	}
	if err := db.Create(batch).Error; err != nil {
		t.Fatalf("create batch: %v", err)
	}
	tx := repository.NewTransactor(db)
	inspectionRepo := repository.NewInspectionRepository(db)
	batchRepo := repository.NewBatchRepository(db)
	releaseRepo := repository.NewReleaseRepository(db)
	inspectionSvc := NewInspectionService(inspectionRepo, batchRepo, noopAudit{}, tx)
	releaseSvc := NewReleaseService(releaseRepo, batchRepo, inspectionRepo, noopAudit{}, tx)
	return db, inspectionSvc, releaseSvc, batch
}

func createReq(batchID uint, code, segment string) dto.CreateInspectionRequest {
	return dto.CreateInspectionRequest{
		ProductionBatchID: batchID, SampleCode: code, Segment: constants.SampleSegment(segment),
		SamplingPosition: "位置", InspectionItem: "热封强度", AcceptanceRange: ">=1.5N",
	}
}

func TestScratchSegmentFlow(t *testing.T) {
	db, inspectionSvc, releaseSvc, batch := newScratchStack(t)
	ctx := context.Background()
	actor := Actor{ID: 1, Name: "检验员", RequestID: "req-1"}

	if _, err := inspectionSvc.Create(ctx, actor, createReq(batch.ID, "S-START", "start")); err != nil {
		t.Fatalf("create start: %v", err)
	}
	// Duplicate segment while pending -> whole registration rejected.
	if _, err := inspectionSvc.Create(ctx, actor, createReq(batch.ID, "S-START-2", "start")); err == nil {
		t.Fatal("duplicate pending segment must be rejected")
	}

	// Release before three segments covered -> rejected, naming missing segments.
	if _, err := releaseSvc.Decide(ctx, Actor{ID: 2, Name: "审批员"}, dto.CreateReleaseDecisionRequest{
		ProductionBatchID: batch.ID, Decision: constants.DecisionRelease, Reason: "三段齐全请放行批次",
	}); err == nil {
		t.Fatal("release with one segment covered must fail")
	}

	// Quarantine is allowed despite missing coverage; reason required by DTO validation.
	if _, err := releaseSvc.Decide(ctx, Actor{ID: 2, Name: "审批员"}, dto.CreateReleaseDecisionRequest{
		ProductionBatchID: batch.ID, Decision: constants.DecisionQuarantine, Reason: "等待中段末段检验先隔离",
	}); err != nil {
		t.Fatalf("quarantine should not require segment coverage: %v", err)
	}
	// Batch now on hold; move it back to running for the rest of the flow.
	if err := db.Model(&model.ProductionBatch{}).Where("id = ?", batch.ID).Update("status", constants.BatchStatusRunning).Error; err != nil {
		t.Fatal(err)
	}

	// Middle and end registered; completing start as pass occupies the slot.
	start, err := inspectionSvc.Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inspectionSvc.Complete(ctx, actor, start.ID, dto.CompleteInspectionRequest{Result: "pass", MeasuredValue: "1.7N"}); err != nil {
		t.Fatalf("complete start pass: %v", err)
	}
	mid, err := inspectionSvc.Create(ctx, actor, createReq(batch.ID, "S-MID", "middle"))
	if err != nil {
		t.Fatalf("create middle: %v", err)
	}
	end, err := inspectionSvc.Create(ctx, actor, createReq(batch.ID, "S-END", "end"))
	if err != nil {
		t.Fatalf("create end: %v", err)
	}
	// Fail the middle sample: its segment slot frees for re-registration.
	if _, err := inspectionSvc.Complete(ctx, actor, mid.ID, dto.CompleteInspectionRequest{Result: "fail", MeasuredValue: "1.1N"}); err != nil {
		t.Fatalf("complete middle fail: %v", err)
	}
	// End passes; the failed middle sample is automatically awaiting retest.
	if _, err := inspectionSvc.Complete(ctx, actor, end.ID, dto.CompleteInspectionRequest{Result: "pass", MeasuredValue: "1.8N"}); err != nil {
		t.Fatalf("complete end pass: %v", err)
	}
	if _, err := releaseSvc.Decide(ctx, Actor{ID: 2, Name: "审批员"}, dto.CreateReleaseDecisionRequest{
		ProductionBatchID: batch.ID, Decision: constants.DecisionRelease, Reason: "三段齐全请放行批次",
	}); err == nil {
		t.Fatal("release while retest pending must fail")
	}

	// Retest the middle sample: completion as pass re-occupies the freed slot.
	if _, err := inspectionSvc.Complete(ctx, actor, mid.ID, dto.CompleteInspectionRequest{Result: "pass", MeasuredValue: "1.65N"}); err != nil {
		t.Fatalf("retest middle pass: %v", err)
	}
	// The historical failed middle sample now passes after retest; registering
	// another middle sample must still be blocked by the occupied slot.
	if _, err := inspectionSvc.Create(ctx, actor, createReq(batch.ID, "S-MID-2", "middle")); err == nil {
		t.Fatal("re-registering an occupied segment must fail")
	}
	decision, err := releaseSvc.Decide(ctx, Actor{ID: 2, Name: "审批员"}, dto.CreateReleaseDecisionRequest{
		ProductionBatchID: batch.ID, Decision: constants.DecisionRelease, Reason: "三段齐全合格同意放行",
	})
	if err != nil {
		t.Fatalf("release after full coverage should succeed: %v", err)
	}
	if decision.Decision != constants.DecisionRelease {
		t.Fatalf("decision = %s", decision.Decision)
	}
}

func TestScratchConcurrentSegmentRegistration(t *testing.T) {
	db, inspectionSvc, _, batch := newScratchStack(t)
	_ = db
	ctx := context.Background()
	actor := Actor{ID: 1, Name: "检验员", RequestID: "req-concurrent"}

	const contenders = 8
	errs := make(chan error, contenders)
	for i := 0; i < contenders; i++ {
		i := i
		go func() {
			code := "S-CONC-" + string(rune('A'+i))
			// SQLite serializes writers and may report a busy lock; the
			// service transaction is retried like a client would.
			var err error
			for attempt := 0; attempt < 20; attempt++ {
				_, err = inspectionSvc.Create(ctx, actor, createReq(batch.ID, code, "end"))
				if err != nil && contains(err.Error(), "locked") {
					time.Sleep(5 * time.Millisecond)
					continue
				}
				break
			}
			errs <- err
		}()
	}
	successes, conflicts := 0, 0
	for i := 0; i < contenders; i++ {
		switch err := <-errs; {
		case err == nil:
			successes++
		case contains(err.Error(), "段位"):
			conflicts++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("expected exactly one success, got %d (conflicts=%d)", successes, conflicts)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
