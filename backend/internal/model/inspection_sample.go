package model

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"

	"sterile-packaging-release-control/backend/internal/constants"
)

var sampleCodePattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{2,49}$`)

type InspectionSample struct {
	Base
	ProductionBatchID uint                    `gorm:"index;not null" json:"productionBatchId"`
	ProductionBatch   ProductionBatch         `json:"productionBatch,omitempty"`
	SampleCode        string                  `gorm:"size:50;uniqueIndex;not null" json:"sampleCode"`
	Segment           constants.SampleSegment `gorm:"size:20;index" json:"segment"`
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

// AfterFind 让段位字段在引入前登记的历史样本上也可用：优先使用存储的段位，
// 缺省时按抽样位置文本兼容识别。
func (s *InspectionSample) AfterFind(tx *gorm.DB) error {
	if s.Segment == "" {
		s.Segment = SegmentFromPosition(s.SamplingPosition)
	}
	return nil
}

// SegmentFromPosition 按抽样位置文本兼容识别旧样本段位；无法判定时返回空串。
// “末”必须先于“中/起始”判断，避免“中末”之类组合被误判；起始优先于中段。
func SegmentFromPosition(position string) constants.SampleSegment {
	text := strings.ToLower(strings.TrimSpace(position))
	if text == "" {
		return ""
	}
	switch {
	case strings.Contains(text, "末") || strings.Contains(text, "尾") || strings.Contains(text, "end"):
		return constants.SegmentEnd
	case strings.Contains(text, "起始") || strings.Contains(text, "开头") || strings.Contains(text, "开始") || strings.Contains(text, "start") || strings.Contains(text, "begin"):
		return constants.SegmentStart
	case strings.Contains(text, "中") || strings.Contains(text, "middle") || strings.Contains(text, "mid"):
		return constants.SegmentMiddle
	default:
		return ""
	}
}

// EffectiveSegment 返回样本生效段位（存储值优先，其次按抽样位置兼容识别）。
func (s InspectionSample) EffectiveSegment() constants.SampleSegment {
	if s.Segment != "" {
		return s.Segment
	}
	return SegmentFromPosition(s.SamplingPosition)
}

// OccupiesSegment 报告样本是否仍占用同批次同一段位的唯一名额：
// 待完成或已合格样本占用；不合格样本（含等待复测）已退出占用，允许重新登记。
func (s InspectionSample) OccupiesSegment() bool {
	return s.Result == "pending" || s.Result == "pass"
}

func (s InspectionSample) ValidateDefinition() error {
	if s.ProductionBatchID == 0 {
		return fmt.Errorf("production batch is required")
	}
	if !sampleCodePattern.MatchString(s.SampleCode) {
		return fmt.Errorf("sample code must contain 3-50 uppercase letters, numbers or hyphens")
	}
	if !s.EffectiveSegment().Valid() {
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
