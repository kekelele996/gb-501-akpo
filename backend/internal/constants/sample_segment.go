package constants

// SampleSegment 标识样本在批次中的抽样段位。
// 枚举同步位置（修改时必须全部更新）：
//   - 后端定义：internal/constants/sample_segment.go
//   - 后端持久化/兼容识别：internal/model/inspection_sample.go、internal/util/database.go
//   - 后端规则：internal/model/production_batch.go、internal/service/inspection_service.go、internal/service/release_service.go
//   - 前端类型：frontend/src/types/domain.ts
//   - 前端工具：frontend/src/utils/segment.ts
//   - 前端公共显示：frontend/src/components/common/SegmentTag.tsx
//   - 前端使用页面：InspectionsPage.tsx、BatchDetailPage.tsx、ReleasePage.tsx、DecisionPanel.tsx
type SampleSegment string

const (
	SegmentStart  SampleSegment = "start"  // 批次起始段
	SegmentMiddle SampleSegment = "middle" // 中段
	SegmentEnd    SampleSegment = "end"    // 末段
)

// AllSegments 按业务顺序返回全部段位，放行覆盖判断按此顺序输出。
func AllSegments() []SampleSegment {
	return []SampleSegment{SegmentStart, SegmentMiddle, SegmentEnd}
}

func (s SampleSegment) Valid() bool {
	for _, candidate := range AllSegments() {
		if s == candidate {
			return true
		}
	}
	return false
}

// Label 返回段位的中文展示名，登记时默认作为抽样位置文本。
func (s SampleSegment) Label() string {
	switch s {
	case SegmentStart:
		return "批次起始段"
	case SegmentMiddle:
		return "批次中段"
	case SegmentEnd:
		return "批次末段"
	default:
		return string(s)
	}
}

// ShortLabel 返回不含“批次”前缀的段位短名，用于“该批次起始段”这类句式。
func (s SampleSegment) ShortLabel() string {
	switch s {
	case SegmentStart:
		return "起始段"
	case SegmentMiddle:
		return "中段"
	case SegmentEnd:
		return "末段"
	default:
		return string(s)
	}
}
