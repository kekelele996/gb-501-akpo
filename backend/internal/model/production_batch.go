package model

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"sterile-packaging-release-control/backend/internal/constants"
)

var batchNumberPattern = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{2,49}$`)

type ProductionBatch struct {
	Base
	BatchNo          string                `gorm:"size:50;uniqueIndex;not null" json:"batchNo"`
	Specification    string                `gorm:"size:180;not null" json:"specification"`
	Status           constants.BatchStatus `gorm:"size:20;index;not null;default:'draft'" json:"status"`
	ResponsibleTeam  string                `gorm:"size:80;not null" json:"responsibleTeam"`
	PackagingLineID  uint                  `gorm:"index;not null" json:"packagingLineId"`
	PackagingLine    PackagingLine         `json:"packagingLine,omitempty"`
	PlannedQuantity  int                   `gorm:"not null;default:0" json:"plannedQuantity"`
	ProducedQuantity int                   `gorm:"not null;default:0" json:"producedQuantity"`
	StartedAt        *time.Time            `json:"startedAt"`
	CompletedAt      *time.Time            `json:"completedAt"`
	HoldReason       string                `gorm:"size:500" json:"holdReason"`
	Inspections      []InspectionSample    `json:"inspections,omitempty"`
	Decisions        []ReleaseDecision     `json:"decisions,omitempty"`
}

func (b *ProductionBatch) Normalize() {
	b.BatchNo = strings.ToUpper(strings.TrimSpace(b.BatchNo))
	b.Specification = strings.TrimSpace(b.Specification)
	b.ResponsibleTeam = strings.TrimSpace(b.ResponsibleTeam)
	b.HoldReason = strings.TrimSpace(b.HoldReason)
}

func (b ProductionBatch) Validate() error {
	if !batchNumberPattern.MatchString(b.BatchNo) {
		return fmt.Errorf("batch number must contain 3-50 uppercase letters, numbers or hyphens")
	}
	if len([]rune(b.Specification)) < 2 || len([]rune(b.Specification)) > 180 {
		return fmt.Errorf("specification must contain 2-180 characters")
	}
	if len([]rune(b.ResponsibleTeam)) < 2 || len([]rune(b.ResponsibleTeam)) > 80 {
		return fmt.Errorf("responsible team must contain 2-80 characters")
	}
	if !b.Status.Valid() {
		return fmt.Errorf("unsupported batch status: %s", b.Status)
	}
	if b.PackagingLineID == 0 {
		return fmt.Errorf("packaging line is required")
	}
	if b.PlannedQuantity < 1 {
		return fmt.Errorf("planned quantity must be positive")
	}
	if b.ProducedQuantity < 0 {
		return fmt.Errorf("produced quantity cannot be negative")
	}
	if b.ProducedQuantity > b.PlannedQuantity*2 {
		return fmt.Errorf("produced quantity exceeds the allowed deviation")
	}
	if len([]rune(b.HoldReason)) > 500 {
		return fmt.Errorf("hold reason cannot exceed 500 characters")
	}
	if b.Status == constants.BatchStatusHold && b.HoldReason == "" {
		return fmt.Errorf("hold reason is required when a batch is on hold")
	}
	return nil
}

func (b ProductionBatch) CompletionPercent() float64 {
	if b.PlannedQuantity <= 0 {
		return 0
	}
	percent := float64(b.ProducedQuantity) / float64(b.PlannedQuantity) * 100
	if percent > 100 {
		return 100
	}
	return percent
}

func (b ProductionBatch) InspectionSummary() (total, passed, failed, pending, retest int) {
	for _, sample := range b.Inspections {
		total++
		switch sample.Result {
		case "pass":
			passed++
		case "fail":
			failed++
		default:
			pending++
		}
		if sample.RetestStatus == "requested" {
			retest++
		}
	}
	return
}

// SegmentCoverage 汇总三段样本的放行覆盖情况。
//
// 段位以有效段位为准（存储段位优先，旧样本按抽样位置兼容识别）。每个段位
// 只要存在一条已合格样本即视为已覆盖；待复测样本会单独统计，不参与覆盖。
func (b ProductionBatch) SegmentCoverage() (passed []constants.SampleSegment, missing []constants.SampleSegment, retest int) {
	passBySegment := make(map[constants.SampleSegment]bool)
	for _, sample := range b.Inspections {
		segment := sample.EffectiveSegment()
		if sample.Result == "pass" && segment.Valid() {
			passBySegment[segment] = true
		}
		if sample.RetestStatus == "requested" {
			retest++
		}
	}
	for _, segment := range constants.AllSegments() {
		if passBySegment[segment] {
			passed = append(passed, segment)
		} else {
			missing = append(missing, segment)
		}
	}
	return passed, missing, retest
}

// MissingSegmentLabels 返回尚未完成合格覆盖的段位中文名，供审批面板与错误提示使用。
func (b ProductionBatch) MissingSegmentLabels() []string {
	_, missing, _ := b.SegmentCoverage()
	labels := make([]string, 0, len(missing))
	for _, segment := range missing {
		labels = append(labels, segment.ShortLabel())
	}
	return labels
}

func (b ProductionBatch) ReadyForRelease() (bool, string) {
	if b.Status == constants.BatchStatusDraft {
		return false, "batch has not started"
	}
	if b.Status == constants.BatchStatusReleased {
		return false, "batch is already released"
	}
	if len(b.Inspections) == 0 {
		return false, "at least one inspection is required"
	}
	_, missing, retest := b.SegmentCoverage()
	if len(missing) > 0 {
		return false, "not all sampling segments have a passed inspection"
	}
	if retest > 0 {
		return false, "requested retests remain"
	}
	return true, ""
}

func (b ProductionBatch) Mutable() bool {
	return b.Status != constants.BatchStatusReleased
}
