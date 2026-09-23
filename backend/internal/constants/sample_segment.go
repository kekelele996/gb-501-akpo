package constants

type SampleSegment string

const (
	SegmentStart  SampleSegment = "start"
	SegmentMiddle SampleSegment = "middle"
	SegmentEnd    SampleSegment = "end"
)

var validSampleSegments = map[SampleSegment]struct{}{
	SegmentStart: {}, SegmentMiddle: {}, SegmentEnd: {},
}

// AllSegments returns the segments in production order: 批次起始段、中段、末段.
func AllSegments() []SampleSegment {
	return []SampleSegment{SegmentStart, SegmentMiddle, SegmentEnd}
}

func (s SampleSegment) Valid() bool {
	_, ok := validSampleSegments[s]
	return ok
}

func (s SampleSegment) Label() string {
	switch s {
	case SegmentStart:
		return "批次起始段"
	case SegmentMiddle:
		return "中段"
	case SegmentEnd:
		return "末段"
	default:
		return string(s)
	}
}
