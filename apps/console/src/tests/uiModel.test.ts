import { describe, expect, it } from 'vitest'
import type { ObservationPolicy, PatrolUnitDto } from '../entities/patrol-unit/model'
import {
  countUntrustedPositions,
  formatAge,
  summarizeStatuses,
  toUiPatrolUnit,
} from '../entities/patrol-unit/uiModel'

const judgedAt = new Date('2026-11-12T09:00:00Z')

const policy: ObservationPolicy = {
  positionStaleAfterSeconds: 45,
  batteryStaleAfterSeconds: 180,
  linkStaleAfterSeconds: 180,
  batteryWarningPercent: 40,
  batteryCriticalPercent: 20,
  linkWarningPercent: 50,
  linkCriticalPercent: 20,
}

function unit(overrides: Partial<PatrolUnitDto> = {}): PatrolUnitDto {
  return {
    id: 'unit-1',
    callsign: 'SG-01',
    displayName: 'Sentinel One',
    status: 'NORMAL',
    position: {
      value: { latitude: 37.5665, longitude: 126.978 },
      observedAt: '2026-11-12T08:59:55Z',
      receivedAt: '2026-11-12T09:00:00Z',
      freshness: 'FRESH',
    },
    battery: {
      value: 82,
      observedAt: '2026-11-12T08:59:55Z',
      receivedAt: '2026-11-12T09:00:00Z',
      freshness: 'FRESH',
    },
    link: {
      value: 94,
      observedAt: '2026-11-12T08:59:55Z',
      receivedAt: '2026-11-12T09:00:00Z',
      freshness: 'FRESH',
    },
    ...overrides,
  }
}

describe('formatAge', () => {
  it.each([
    ['2026-11-12T08:59:55Z', '5초 전'],
    ['2026-11-12T08:40:00Z', '20분 전'],
    ['2026-11-12T07:00:00Z', '2시간 전'],
    ['2026-11-12T06:30:00Z', '2시간 30분 전'],
  ])('formats %s as %s', (timestamp, expected) => {
    expect(formatAge(timestamp, judgedAt)).toBe(expected)
  })

  it('has no age without a timestamp', () => {
    expect(formatAge(null, judgedAt)).toBeNull()
  })
})

describe('toUiPatrolUnit', () => {
  it('trusts a fresh observation and explains a healthy unit', () => {
    const view = toUiPatrolUnit(unit(), policy, judgedAt)

    expect(view.tone).toBe('normal')
    expect(view.statusLabel).toBe('정상')
    expect(view.battery.trusted).toBe(true)
    expect(view.battery.valueLabel).toBe('82%')
    expect(view.evidenceGaps).toEqual([])
    expect(view.position.mappable).toBe(true)
  })

  it('keeps a stale value visible but not trusted', () => {
    const stale = unit({
      status: 'UNKNOWN',
      position: {
        value: { latitude: 37.5685, longitude: 126.982 },
        observedAt: '2026-11-12T08:40:00Z',
        receivedAt: '2026-11-12T08:40:00Z',
        freshness: 'STALE',
      },
    })

    const view = toUiPatrolUnit(stale, policy, judgedAt)

    expect(view.position.trusted).toBe(false)
    expect(view.position.mappable).toBe(true)
    expect(view.position.valueLabel).toBe('37.5685, 126.9820')
    expect(view.position.observedAgoLabel).toBe('20분 전')
    expect(view.evidenceGaps).toEqual(['위치 관측 지연'])
    expect(view.statusExplanation).toBe('근거 부족: 위치 관측 지연')
  })

  it('names every field that leaves the condition unknown', () => {
    const view = toUiPatrolUnit(
      unit({
        status: 'UNKNOWN',
        battery: { value: null, observedAt: null, receivedAt: null, freshness: 'MISSING' },
        link: {
          value: 72,
          observedAt: '2026-11-12T08:40:00Z',
          receivedAt: '2026-11-12T08:40:00Z',
          freshness: 'STALE',
        },
      }),
      policy,
      judgedAt,
    )

    expect(view.evidenceGaps).toEqual(['배터리 관측 누락', '링크 관측 지연'])
    expect(view.battery.valueLabel).toBe('관측 없음')
  })

  it('explains a warning with the site threshold that produced it', () => {
    const view = toUiPatrolUnit(
      unit({
        status: 'WARNING',
        battery: {
          value: 35,
          observedAt: '2026-11-12T08:59:55Z',
          receivedAt: '2026-11-12T09:00:00Z',
          freshness: 'FRESH',
        },
      }),
      policy,
      judgedAt,
    )

    expect(view.statusExplanation).toBe('배터리 35% (주의 기준 40% 이하)')
  })

  it('explains a critical unit with the critical threshold', () => {
    const view = toUiPatrolUnit(
      unit({
        status: 'CRITICAL',
        battery: {
          value: 14,
          observedAt: '2026-11-12T08:59:55Z',
          receivedAt: '2026-11-12T09:00:00Z',
          freshness: 'FRESH',
        },
      }),
      policy,
      judgedAt,
    )

    expect(view.statusExplanation).toBe('배터리 14% (위험 기준 20% 이하)')
  })

  it('cannot map a unit whose position is absent', () => {
    const view = toUiPatrolUnit(
      unit({
        status: 'UNKNOWN',
        position: {
          value: null,
          observedAt: null,
          receivedAt: '2026-11-12T08:20:00Z',
          freshness: 'MISSING',
        },
      }),
      policy,
      judgedAt,
    )

    expect(view.position.mappable).toBe(false)
    expect(view.position.valueLabel).toBe('위치 없음')
    expect(view.position.receivedAgoLabel).toBe('40분 전')
  })
})

describe('summaries', () => {
  const views = [
    toUiPatrolUnit(unit({ id: 'a', status: 'NORMAL' }), policy, judgedAt),
    toUiPatrolUnit(unit({ id: 'b', status: 'WARNING' }), policy, judgedAt),
    toUiPatrolUnit(
      unit({
        id: 'c',
        status: 'UNKNOWN',
        position: {
          value: null,
          observedAt: null,
          receivedAt: '2026-11-12T08:20:00Z',
          freshness: 'MISSING',
        },
      }),
      policy,
      judgedAt,
    ),
  ]

  it('counts units per condition', () => {
    expect(summarizeStatuses(views)).toEqual({ NORMAL: 1, WARNING: 1, CRITICAL: 0, UNKNOWN: 1 })
  })

  it('counts positions that cannot be trusted as current', () => {
    expect(countUntrustedPositions(views)).toBe(1)
  })
})
