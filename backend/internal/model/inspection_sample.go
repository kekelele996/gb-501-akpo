package model

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"sterile-packaging-release-control/backend/internal/constants"
)

var sampleCodePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{2,49}$`)

type InspectionSample struct {
	Base
	ProductionBatchID uint                    `gorm:"index;not null" json:"productionBatchId"`
	ProductionBatch   ProductionBatch         `json:"productionBatch,omitempty"`
	SampleCode        string                  `gorm:"size:50;uniqueIndex;not null" json:"sampleCode"`
	Segment           constants.SampleSegment `gorm:"size:20;index;not null;default:''" json:"segment"`
	SamplingPosition  string                  `gorm:"size:120;not null" json:"samplingPosition"`
	InspectionItem    string                  `gorm:"size:120;not null" json:"inspectionItem"`
	Result            string                  `gorm:"size:20;index;not null;default:'pending'" json:"result"`
	MeasuredValue     string                  `gorm:"size:100" json:"measuredValue"`
	AcceptanceRange   string                  `gorm:"size:100" json:"acceptanceRange"`
	RetestStatus      string                  `gorm:"size:20;not null;default:'none'" json:"retestStatus"`
	InspectorID       uint                    `gorm:"index" json:"inspectorId"`
	InspectorName     string                  `gorm:"size:100" json:"inspectorName"`
	InspectedAt       *time.Time              `json:"inspectedAt"`
	Notes             string                  `gorm:"size:1000" json:"notes"`
}

func (s *InspectionSample) Normalize() {
	s.SampleCode = strings.ToUpper(strings.TrimSpace(s.SampleCode))
	s.Segment = constants.SampleSegment(strings.ToLower(strings.TrimSpace(string(s.Segment))))
	s.SamplingPosition = strings.TrimSpace(s.SamplingPosition)
	s.InspectionItem = strings.TrimSpace(s.InspectionItem)
	s.Result = strings.ToLower(strings.TrimSpace(s.Result))
	s.MeasuredValue = strings.TrimSpace(s.MeasuredValue)
	s.AcceptanceRange = strings.TrimSpace(s.AcceptanceRange)
	s.RetestStatus = strings.ToLower(strings.TrimSpace(s.RetestStatus))
	s.InspectorName = strings.TrimSpace(s.InspectorName)
	s.Notes = strings.TrimSpace(s.Notes)
}

// InferSegment keeps legacy samples identifiable: their segment is derived
// from the sampling position text recorded before the segment field existed.
func InferSegment(samplingPosition string) constants.SampleSegment {
	position := strings.ToLower(strings.TrimSpace(samplingPosition))
	switch {
	case position == "":
		return ""
	case strings.Contains(position, "起始") || strings.Contains(position, "开始") || strings.Contains(position, "start") || strings.Contains(position, "begin"):
		return constants.SegmentStart
	case strings.Contains(position, "末") || strings.Contains(position, "终") || strings.Contains(position, "尾") || strings.Contains(position, "end") || strings.Contains(position, "finish"):
		return constants.SegmentEnd
	case strings.Contains(position, "中") || strings.Contains(position, "middle") || strings.Contains(position, "mid"):
		return constants.SegmentMiddle
	default:
		return ""
	}
}

// EffectiveSegment returns the stored segment or, for samples registered
// before the segment field existed, a segment inferred from position text.
func (s InspectionSample) EffectiveSegment() constants.SampleSegment {
	if s.Segment.Valid() {
		return s.Segment
	}
	return InferSegment(s.SamplingPosition)
}

func (s InspectionSample) ValidateDefinition() error {
	if s.ProductionBatchID == 0 {
		return fmt.Errorf("production batch is required")
	}
	if !sampleCodePattern.MatchString(s.SampleCode) {
		return fmt.Errorf("sample code must contain 3-50 uppercase letters, numbers or hyphens")
	}
	// Legacy rows may still carry an empty segment; accept them when the
	// sampling position can be mapped onto one of the three segments.
	if !s.Segment.Valid() && !InferSegment(s.SamplingPosition).Valid() {
		return fmt.Errorf("segment must be one of start, middle or end")
	}
	if s.SamplingPosition == "" || len([]rune(s.SamplingPosition)) > 120 {
		return fmt.Errorf("sampling position must contain 1-120 characters")
	}
	if s.InspectionItem == "" || len([]rune(s.InspectionItem)) > 120 {
		return fmt.Errorf("inspection item must contain 1-120 characters")
	}
	if s.AcceptanceRange == "" || len([]rune(s.AcceptanceRange)) > 100 {
		return fmt.Errorf("acceptance range must contain 1-100 characters")
	}
	if len([]rune(s.Notes)) > 1000 {
		return fmt.Errorf("notes cannot exceed 1000 characters")
	}
	return s.ValidateState()
}

func (s InspectionSample) ValidateState() error {
	switch s.Result {
	case "pending", "pass", "fail":
	default:
		return fmt.Errorf("unsupported inspection result: %s", s.Result)
	}
	switch s.RetestStatus {
	case "none", "requested", "completed":
	default:
		return fmt.Errorf("unsupported retest status: %s", s.RetestStatus)
	}
	if s.Result != "pending" && s.MeasuredValue == "" {
		return fmt.Errorf("measured value is required for a completed inspection")
	}
	if s.Result == "pending" && s.InspectedAt != nil {
		return fmt.Errorf("a pending inspection cannot have an inspection timestamp")
	}
	if s.RetestStatus == "requested" && s.Result != "fail" {
		return fmt.Errorf("retest can only be requested for a failed result")
	}
	return nil
}

func (s InspectionSample) Completed() bool {
	return s.Result == "pass" || s.Result == "fail"
}

func (s InspectionSample) BlocksRelease() bool {
	return s.Result != "pass" || s.RetestStatus == "requested"
}

// OccupiesSegment reports whether the sample keeps a segment slot occupied:
// a pending sample awaiting completion or a qualified sample. A failed sample
// frees its segment so another sample can be registered for that segment.
func (s InspectionSample) OccupiesSegment() bool {
	return s.Result == "pending" || s.Result == "pass"
}
