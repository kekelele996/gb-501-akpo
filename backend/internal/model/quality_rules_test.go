package model

import (
	"testing"
	"time"

	"sterile-packaging-release-control/backend/internal/constants"
)

func validBatch() ProductionBatch {
	return ProductionBatch{
		BatchNo: "B20260822-TEST", Specification: "无菌屏障袋", Status: constants.BatchStatusRunning,
		ResponsibleTeam: "验证班", PackagingLineID: 1, PlannedQuantity: 1000, ProducedQuantity: 800,
	}
}

func passedSample(segment constants.SampleSegment) InspectionSample {
	return InspectionSample{Segment: segment, Result: "pass", MeasuredValue: "1.7 N", RetestStatus: "none"}
}

func TestBatchReadyForReleaseRequiresAllSegments(t *testing.T) {
	batch := validBatch()
	if ready, _ := batch.ReadyForRelease(); ready {
		t.Fatal("batch without inspections cannot release")
	}
	batch.Inspections = []InspectionSample{passedSample(constants.SegmentStart)}
	if ready, _ := batch.ReadyForRelease(); ready {
		t.Fatal("batch covering only start segment cannot release")
	}
	batch.Inspections = []InspectionSample{
		passedSample(constants.SegmentStart),
		passedSample(constants.SegmentMiddle),
		passedSample(constants.SegmentEnd),
	}
	if ready, reason := batch.ReadyForRelease(); !ready {
		t.Fatalf("all-segment passed batch should release: %s", reason)
	}
	// 末段存在待复测时即使另外两段合格也不能放行。
	batch.Inspections = append(batch.Inspections, InspectionSample{
		Segment: constants.SegmentEnd, Result: "fail", RetestStatus: "requested",
	})
	if ready, _ := batch.ReadyForRelease(); ready {
		t.Fatal("batch with requested retest cannot release")
	}
}

func TestSegmentCoverageListsMissingSegments(t *testing.T) {
	batch := validBatch()
	batch.Inspections = []InspectionSample{
		{Segment: constants.SegmentStart, Result: "pass", RetestStatus: "none"},
		{Segment: constants.SegmentEnd, Result: "pending", RetestStatus: "none"},
	}
	passed, missing, retest := batch.SegmentCoverage()
	if len(passed) != 1 || passed[0] != constants.SegmentStart {
		t.Fatalf("unexpected passed segments: %v", passed)
	}
	if len(missing) != 2 || missing[0] != constants.SegmentMiddle || missing[1] != constants.SegmentEnd {
		t.Fatalf("pending end sample must not count as coverage, got missing %v", missing)
	}
	if retest != 0 {
		t.Fatalf("unexpected retest count %d", retest)
	}
}

func TestSegmentFromPositionIsBackwardCompatible(t *testing.T) {
	cases := map[string]constants.SampleSegment{
		"批次起始段":   constants.SegmentStart,
		"起始位置":    constants.SegmentStart,
		"START-A": constants.SegmentStart,
		"批次中段":    constants.SegmentMiddle,
		"产线中部":    constants.SegmentMiddle,
		"MIDDLE":  constants.SegmentMiddle,
		"批次末段":    constants.SegmentEnd,
		"线尾抽样":    constants.SegmentEnd,
		"END-01":  constants.SegmentEnd,
		"随机点位":    "",
		"":        "",
	}
	for position, want := range cases {
		if got := SegmentFromPosition(position); got != want {
			t.Fatalf("SegmentFromPosition(%q) = %q, want %q", position, got, want)
		}
	}
}

func TestLegacySampleDerivesSegmentFromPosition(t *testing.T) {
	legacy := InspectionSample{SamplingPosition: "批次中段"}
	if legacy.EffectiveSegment() != constants.SegmentMiddle {
		t.Fatal("legacy sample should derive segment from sampling position")
	}
	// AfterFind 后存储段位被填充，前端读取到的段位与覆盖进度保持一致。
	if err := legacy.AfterFind(nil); err != nil {
		t.Fatalf("AfterFind failed: %v", err)
	}
	if legacy.Segment != constants.SegmentMiddle {
		t.Fatalf("AfterFind should populate segment, got %q", legacy.Segment)
	}
	explicit := InspectionSample{Segment: constants.SegmentEnd, SamplingPosition: "起始位置"}
	if explicit.EffectiveSegment() != constants.SegmentEnd {
		t.Fatal("stored segment must take precedence over sampling position")
	}
}

func TestOccupiesSegment(t *testing.T) {
	if !(InspectionSample{Result: "pending"}).OccupiesSegment() {
		t.Fatal("pending sample occupies its segment")
	}
	if !(InspectionSample{Result: "pass"}).OccupiesSegment() {
		t.Fatal("passed sample occupies its segment")
	}
	if (InspectionSample{Result: "fail", RetestStatus: "requested"}).OccupiesSegment() {
		t.Fatal("failed sample releases its segment for re-registration")
	}
}

func TestInspectionValidation(t *testing.T) {
	now := time.Now()
	sample := InspectionSample{
		ProductionBatchID: 1, SampleCode: "SAMPLE-001", Segment: constants.SegmentMiddle, SamplingPosition: "批次中段",
		InspectionItem: "热封强度", Result: "pass", MeasuredValue: "1.7 N",
		AcceptanceRange: ">= 1.5 N", RetestStatus: "none", InspectedAt: &now,
	}
	if err := sample.ValidateDefinition(); err != nil {
		t.Fatalf("valid sample rejected: %v", err)
	}
	// 没有段位且抽样位置无法识别时校验失败。
	noSegment := sample
	noSegment.Segment = ""
	noSegment.SamplingPosition = "随机点位"
	if err := noSegment.ValidateDefinition(); err == nil {
		t.Fatal("sample without recognizable segment must fail validation")
	}
	// 旧样本只提供可识别的抽样位置时仍然合法（兼容登记数据）。
	legacy := sample
	legacy.Segment = ""
	legacy.SamplingPosition = "末段"
	if err := legacy.ValidateDefinition(); err != nil {
		t.Fatalf("legacy sample with recognizable position rejected: %v", err)
	}
	sample.Result = "pending"
	if err := sample.ValidateDefinition(); err == nil {
		t.Fatal("pending sample with timestamp must fail")
	}
}
