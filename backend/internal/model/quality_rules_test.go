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
	return InspectionSample{Segment: segment, Result: "pass", RetestStatus: "none"}
}

func TestBatchReadyForRelease(t *testing.T) {
	batch := validBatch()
	if ready, _ := batch.ReadyForRelease(); ready {
		t.Fatal("batch without inspections cannot release")
	}
	batch.Inspections = []InspectionSample{
		passedSample(constants.SegmentStart),
		passedSample(constants.SegmentMiddle),
		passedSample(constants.SegmentEnd),
	}
	if ready, reason := batch.ReadyForRelease(); !ready {
		t.Fatalf("batch with three passed segments should release: %s", reason)
	}
	// A failed sample with a completed retest no longer blocks release once
	// every segment is covered by a qualified sample.
	batch.Inspections = append(batch.Inspections, InspectionSample{
		Segment: constants.SegmentStart, Result: "fail", RetestStatus: "completed",
	})
	if ready, _ := batch.ReadyForRelease(); !ready {
		t.Fatal("completed-retest failure alongside full segment coverage should release")
	}
}

func TestReadyForReleaseRequiresAllSegmentsAndNoRetest(t *testing.T) {
	batch := validBatch()
	batch.Inspections = []InspectionSample{
		passedSample(constants.SegmentStart),
		passedSample(constants.SegmentEnd),
	}
	ready, reason := batch.ReadyForRelease()
	if ready || reason == "" {
		t.Fatal("missing middle segment must block release")
	}
	batch.Inspections = append(batch.Inspections, InspectionSample{
		Segment: constants.SegmentMiddle, Result: "fail", RetestStatus: "requested",
	})
	if ready, _ := batch.ReadyForRelease(); ready {
		t.Fatal("requested retest must block release")
	}
}

func TestSegmentCoverage(t *testing.T) {
	batch := validBatch()
	batch.Inspections = []InspectionSample{
		{Segment: constants.SegmentStart, Result: "pass"},
		{Segment: constants.SegmentMiddle, Result: "pending"},
		// Legacy sample with no stored segment but an inferable position.
		{Segment: "", SamplingPosition: "成品卷末尾", Result: "pass"},
	}
	coverage := batch.SegmentCoverage()
	if !coverage[constants.SegmentStart] || !coverage[constants.SegmentEnd] {
		t.Fatalf("start and end should be covered: %#v", coverage)
	}
	if coverage[constants.SegmentMiddle] {
		t.Fatal("pending middle sample must not count as covered")
	}
	missing := batch.MissingReleaseSegments()
	if len(missing) != 1 || missing[0] != constants.SegmentMiddle {
		t.Fatalf("only middle should be missing, got %#v", missing)
	}
}

func TestInspectionValidation(t *testing.T) {
	now := time.Now()
	sample := InspectionSample{
		ProductionBatchID: 1, SampleCode: "SAMPLE-001", Segment: constants.SegmentMiddle, SamplingPosition: "中段",
		InspectionItem: "热封强度", Result: "pass", MeasuredValue: "1.7 N",
		AcceptanceRange: ">= 1.5 N", RetestStatus: "none", InspectedAt: &now,
	}
	if err := sample.ValidateDefinition(); err != nil {
		t.Fatalf("valid sample rejected: %v", err)
	}
	sample.Result = "pending"
	if err := sample.ValidateDefinition(); err == nil {
		t.Fatal("pending sample with timestamp must fail")
	}
}

func TestSegmentRequiredForNewSamples(t *testing.T) {
	sample := InspectionSample{
		ProductionBatchID: 1, SampleCode: "SAMPLE-002", SamplingPosition: "封边 3 号位",
		InspectionItem: "外观", Result: "pending", RetestStatus: "none", AcceptanceRange: "无缺陷",
	}
	if err := sample.ValidateDefinition(); err == nil {
		t.Fatal("sample without segment and without inferable position must fail")
	}
	// Legacy samples with an inferable position remain valid.
	legacy := sample
	legacy.SamplingPosition = "批次起始段取样"
	if err := legacy.ValidateDefinition(); err != nil {
		t.Fatalf("legacy sample with inferable start position rejected: %v", err)
	}
	if legacy.EffectiveSegment() != constants.SegmentStart {
		t.Fatalf("effective segment = %s, want start", legacy.EffectiveSegment())
	}
}

func TestInferSegment(t *testing.T) {
	cases := map[string]constants.SampleSegment{
		"批次起始段":        constants.SegmentStart,
		"开始位置":         constants.SegmentStart,
		"START-01":     constants.SegmentStart,
		"批次中段":         constants.SegmentMiddle,
		"中部 MID":       constants.SegmentMiddle,
		"批次末段":         constants.SegmentEnd,
		"成品卷尾端":        constants.SegmentEnd,
		"finished end": constants.SegmentEnd,
		"任意位置":         "",
	}
	for position, want := range cases {
		if got := InferSegment(position); got != want {
			t.Fatalf("InferSegment(%q) = %q, want %q", position, got, want)
		}
	}
}

func TestOccupiesSegment(t *testing.T) {
	if !((InspectionSample{Result: "pending"}).OccupiesSegment()) {
		t.Fatal("pending sample must occupy its segment")
	}
	if !((InspectionSample{Result: "pass"}).OccupiesSegment()) {
		t.Fatal("passed sample must occupy its segment")
	}
	if (InspectionSample{Result: "fail"}).OccupiesSegment() {
		t.Fatal("failed sample must release its segment slot")
	}
}
