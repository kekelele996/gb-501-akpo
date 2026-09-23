import type { InspectionSample, SampleSegment } from '../types/domain'

export const SEGMENT_ORDER: SampleSegment[] = ['start', 'middle', 'end']

export const SEGMENT_OPTIONS = [
  { value: 'start', label: '批次起始段' },
  { value: 'middle', label: '中段' },
  { value: 'end', label: '末段' },
] as const

export const SEGMENT_LABELS: Record<SampleSegment, string> = {
  start: '批次起始段',
  middle: '中段',
  end: '末段',
}

const POSITION_KEYWORDS: Array<{ segment: SampleSegment; keywords: string[] }> = [
  { segment: 'start', keywords: ['起始', '开始', 'start', 'begin'] },
  { segment: 'end', keywords: ['末', '终', '尾', 'end', 'finish'] },
  { segment: 'middle', keywords: ['中', 'middle', 'mid'] },
]

// inferSegment keeps samples registered before the segment field existed
// recognizable from their sampling position text.
export function inferSegment(samplingPosition: string): SampleSegment | '' {
  const position = samplingPosition.trim().toLowerCase()
  if (!position) return ''
  for (const { segment, keywords } of POSITION_KEYWORDS) {
    if (keywords.some((keyword) => position.includes(keyword))) return segment
  }
  return ''
}

export function effectiveSegment(sample: InspectionSample): SampleSegment | '' {
  return sample.segment || inferSegment(sample.samplingPosition)
}

export function segmentCoverage(samples: InspectionSample[] = []): Record<SampleSegment, boolean> {
  const covered: Record<SampleSegment, boolean> = { start: false, middle: false, end: false }
  for (const sample of samples) {
    const segment = effectiveSegment(sample)
    if (segment && sample.result === 'pass') covered[segment] = true
  }
  return covered
}

export function missingSegments(samples: InspectionSample[] = []): SampleSegment[] {
  const covered = segmentCoverage(samples)
  return SEGMENT_ORDER.filter((segment) => !covered[segment])
}

// occupiedSegments lists segments that already hold a pending or qualified
// sample; registering another such sample for these segments is rejected.
export function occupiedSegments(samples: InspectionSample[] = [], batchId?: number): SampleSegment[] {
  const occupied = new Set<SampleSegment>()
  for (const sample of samples) {
    if (batchId !== undefined && sample.productionBatchId !== batchId) continue
    if (sample.result === 'pending' || sample.result === 'pass') {
      const segment = effectiveSegment(sample)
      if (segment) occupied.add(segment)
    }
  }
  return SEGMENT_ORDER.filter((segment) => occupied.has(segment))
}

export function hasRequestedRetest(samples: InspectionSample[] = []): boolean {
  return samples.some((sample) => sample.retestStatus === 'requested')
}
