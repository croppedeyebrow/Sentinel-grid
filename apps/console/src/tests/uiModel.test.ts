import { describe, expect, it } from 'vitest'
import type { ObservationPolicy, PatrolUnitDto } from '../entities/patrol-unit/model'
import {
  countSilentStreams,
  countUntrustedPositions,
  formatAge,
  summarizeStatuses,
  toUiIngest,
  toUiPatrolUnit,
  toUiStream,
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
    stream: {
      state: 'STREAMING',
      sessionId: 'session-1',
      sessionStartedAt: '2026-11-12T08:30:00Z',
      lastReceivedAt: '2026-11-12T09:00:00Z',
      reconnected: false,
      missingFrames: 0,
      rejectedFrames: 0,
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

describe('toUiStream', () => {
  const streaming = {
    state: 'STREAMING' as const,
    sessionId: 'session-1',
    sessionStartedAt: '2026-11-12T08:30:00Z',
    lastReceivedAt: '2026-11-12T08:59:58Z',
    reconnected: false,
    missingFrames: 0,
    rejectedFrames: 0,
  }

  it('says frames are arriving and nothing is wrong', () => {
    const stream = toUiStream(streaming, judgedAt)

    expect(stream.arriving).toBe(true)
    expect(stream.label).toBe('수신 중')
    expect(stream.lastReceivedAgoLabel).toBe('2초 전')
    expect(stream.warning).toBe('')
  })

  it('names silence as the problem it is', () => {
    const stream = toUiStream(
      { ...streaming, state: 'SILENT', lastReceivedAt: '2026-11-12T08:20:00Z' },
      judgedAt,
    )

    expect(stream.arriving).toBe(false)
    expect(stream.label).toBe('수신 끊김')
    expect(stream.lastReceivedAgoLabel).toBe('40분 전')
    expect(stream.warning).toContain('도착하지 않습니다')
  })

  it('reports an incomplete stream and refused frames together', () => {
    const stream = toUiStream({ ...streaming, missingFrames: 3, rejectedFrames: 2 }, judgedAt)

    expect(stream.warning).toContain('누락 3프레임')
    expect(stream.warning).toContain('거부 2프레임')
  })

  it('distinguishes a unit never heard from', () => {
    const stream = toUiStream(
      {
        state: 'NEVER_SEEN',
        sessionId: null,
        sessionStartedAt: null,
        lastReceivedAt: null,
        reconnected: false,
        missingFrames: 0,
        rejectedFrames: 0,
      },
      judgedAt,
    )

    expect(stream.label).toBe('수신 기록 없음')
    expect(stream.lastReceivedAgoLabel).toBeNull()
    expect(stream.warning).toContain('수신 기록이 없습니다')
  })

  it('passes a reconnect through for the screen to mark', () => {
    expect(toUiStream({ ...streaming, reconnected: true }, judgedAt).reconnected).toBe(true)
  })
})

describe('countSilentStreams', () => {
  it('counts units whose telemetry is not arriving, regardless of condition', () => {
    const views = [
      toUiPatrolUnit(unit({ id: 'a' }), policy, judgedAt),
      toUiPatrolUnit(
        unit({
          id: 'b',
          stream: {
            state: 'SILENT',
            sessionId: 'session-2',
            sessionStartedAt: '2026-11-12T07:50:00Z',
            lastReceivedAt: '2026-11-12T08:20:00Z',
            reconnected: false,
            missingFrames: 7,
            rejectedFrames: 0,
          },
        }),
        policy,
        judgedAt,
      ),
    ]

    expect(countSilentStreams(views)).toBe(1)
  })
})

describe('toUiIngest', () => {
  const now = new Date('2026-11-12T09:00:00Z')

  it('treats a working intake as the platform saying nothing is wrong', () => {
    const ingest = toUiIngest(
      { state: 'RECEIVING', lastEventAt: '2026-11-12T08:59:50Z' },
      now,
    )

    expect(ingest.receiving).toBe(true)
    expect(ingest.silenceIsOurs).toBe(false)
    expect(ingest.warning).toBe('')
    expect(ingest.lastEventAgoLabel).not.toBeNull()
  })

  it('says the silence may be ours when the platform is not receiving', () => {
    // The point of the whole field: without it a broken pipe reads as a lost
    // fleet, because every unit goes silent at once.
    const ingest = toUiIngest({ state: 'NOT_RECEIVING', lastEventAt: null }, now)

    expect(ingest.receiving).toBe(false)
    expect(ingest.silenceIsOurs).toBe(true)
    expect(ingest.warning).toContain('순찰체 문제가 아닐 수 있습니다')
    expect(ingest.lastEventAgoLabel).toBeNull()
  })

  it('does not treat an unknown intake as a healthy one', () => {
    const ingest = toUiIngest({ state: 'UNKNOWN', lastEventAt: null }, now)

    expect(ingest.receiving).toBe(false)
    expect(ingest.silenceIsOurs).toBe(true)
    expect(ingest.warning).not.toBe('')
  })
})
