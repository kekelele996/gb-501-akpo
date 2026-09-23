import { Tag } from 'antd'
import type { SampleSegment } from '../../types/domain'
import { SEGMENT_COLORS, SEGMENT_LABELS, SEGMENT_SHORT_LABELS, effectiveSegment } from '../../utils/segment'

// 段位标签：新样本显示存储段位，段位上线前的旧样本按抽样位置兼容识别。
export function SegmentTag({ sample, short = false }: { sample: { segment?: SampleSegment; samplingPosition: string }; short?: boolean }) {
  const segment = effectiveSegment(sample)
  if (!segment) return <Tag>{sample.samplingPosition || '-'}</Tag>
  return <Tag color={SEGMENT_COLORS[segment]}>{short ? SEGMENT_SHORT_LABELS[segment] : SEGMENT_LABELS[segment]}</Tag>
}
