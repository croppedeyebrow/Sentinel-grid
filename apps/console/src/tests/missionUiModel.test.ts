import { describe, expect, it } from 'vitest'
import { mockMissions, mockPatrolArea } from '../entities/mission/mockMissions'
import {
  allowedTransitions,
  conflictingUnits,
  readCoverage,
} from '../entities/mission/uiModel'
import { isInsideArea } from '../shared/geo/polygon'

describe('readCoverage', () => {
  it('reports the three judgements separately and never folds the undecided ones', () => {
    const reading = readCoverage({ visited: 2, missed: 1, unknown: 1, planned: 1, total: 5 })

    expect(reading.visitedPercent).toBe(40)
    expect(reading.missedPercent).toBe(20)
    expect(reading.unknownPercent).toBe(20)
    // The three percentages do not add up to 100 while checkpoints remain
    // unjudged, which is the honest reading.
    expect(reading.visitedPercent + reading.missedPercent + reading.unknownPercent).toBeLessThan(100)
    expect(reading.judged).toBe(3)
    expect(reading.label).toBe('통과 2 · 미순찰 1 · 판단 불가 1 / 5')
  })

  it('does not divide by zero for a plan without checkpoints', () => {
    const reading = readCoverage({ visited: 0, missed: 0, unknown: 0, planned: 0, total: 0 })

    expect(reading.visitedPercent).toBe(0)
    expect(reading.label).toBe('체크포인트 없음')
  })
})

describe('allowedTransitions', () => {
  it.each([
    ['DRAFT', ['approve', 'abort']],
    ['READY', ['start', 'abort']],
    ['ACTIVE', ['complete', 'abort']],
    ['COMPLETED', []],
    ['ABORTED', []],
  ] as const)('offers %s → %j', (status, expected) => {
    expect(allowedTransitions(status)).toEqual(expected)
  })
})

describe('conflictingUnits', () => {
  const active = mockMissions[0]

  it('finds a unit already committed to an overlapping window', () => {
    const candidate = {
      ...active,
      id: 'other',
      window: { start: '2026-11-21T10:00:00Z', end: '2026-11-21T12:00:00Z' },
    }

    expect(conflictingUnits(mockMissions, candidate)).toEqual(['SG-01'])
  })

  it('ignores windows that do not overlap', () => {
    const candidate = {
      ...active,
      id: 'other',
      window: { start: '2026-11-21T11:00:00Z', end: '2026-11-21T13:00:00Z' },
    }

    expect(conflictingUnits(mockMissions, candidate)).toEqual([])
  })

  it('ignores missions that already ended', () => {
    const finished = mockMissions.find((mission) => mission.status === 'COMPLETED')!
    const candidate = { ...finished, id: 'other', status: 'DRAFT' as const }

    expect(conflictingUnits(mockMissions, candidate)).toEqual([])
  })
})

describe('isInsideArea', () => {
  it.each([
    [{ latitude: 37.5665, longitude: 126.978 }, true],
    [{ latitude: 37.561, longitude: 126.97 }, true],
    [{ latitude: 37.6, longitude: 127.05 }, false],
    [{ latitude: 37.55, longitude: 126.978 }, false],
  ])('judges %j as inside=%s', (point, expected) => {
    expect(isInsideArea(point, mockPatrolArea)).toBe(expected)
  })
})
