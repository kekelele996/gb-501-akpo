import type { InspectionSample, SampleSegment } from '../types/domain'

// 段位枚举同步位置见后端 internal/constants/sample_segment.go 注释；
// 修改任一处时前后端必须同步。
export const SEGMENT_ORDER: SampleSegment[] = ['start', 'middle', 'end']

export const SEGMENT_LABELS: Record<SampleSegment, string> = {
  start: '批次起始段',
  middle: '批次中段',
  end: '批次末段',
}

export const SEGMENT_SHORT_LABELS: Record<SampleSegment, string> = {
  start: '起始段',
  middle: '中段',
  end: '末段',
}

export const SEGMENT_COLORS: Record<SampleSegment, string> = {
  start: 'blue',
  middle: 'gold',
  end: 'purple',
}

// segmentFromPosition 与后端 model.SegmentFromPosition 保持一致：
// 优先识别“末/尾/end”，再识别“起始/开始/start”，最后识别“中/middle”。
export function segmentFromPosition(position?: string): SampleSegment | undefined {
  const text = (position || '').trim().toLowerCase()
  if (!text) return undefined
  if (text.includes('末') || text.includes('尾') || text.includes('end')) return 'end'
  if (text.includes('起始') || text.includes('开头') || text.includes('开始') || text.includes('start') || text.includes('begin')) return 'start'
  if (text.includes('中') || text.includes('middle') || text.includes('mid')) return 'middle'
  return undefined
}

// 旧样本没有段位字段时按抽样位置兼容识别，保证刷新后段位与后端覆盖进度一致。
export function effectiveSegment(sample: Pick<InspectionSample, 'segment' | 'samplingPosition'>): SampleSegment | undefined {
  return sample.segment || segmentFromPosition(sample.samplingPosition)
}

// 待完成或已合格样本占用段位名额；不合格（含待复测）不占用，可重新登记。
export function occupiesSegment(sample: Pick<InspectionSample, 'result'>): boolean {
  return sample.result === 'pending' || sample.result === 'pass'
}

export interface SegmentCoverage {
  passed: SampleSegment[]
  missing: SampleSegment[]
  retest: number
}

// 汇总批次三段覆盖：每段存在已合格样本即视为覆盖，待复测单独计数。
export function segmentCoverage(samples: InspectionSample[] | undefined): SegmentCoverage {
  const passedSet = new Set<SampleSegment>()
  let retest = 0
  for (const sample of samples || []) {
    const segment = effectiveSegment(sample)
    if (sample.result === 'pass' && segment) passedSet.add(segment)
    if (sample.retestStatus === 'requested') retest += 1
  }
  const passed = SEGMENT_ORDER.filter((segment) => passedSet.has(segment))
  const missing = SEGMENT_ORDER.filter((segment) => !passedSet.has(segment))
  return { passed, missing, retest }
}
