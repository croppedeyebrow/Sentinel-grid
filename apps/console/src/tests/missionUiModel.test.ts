import { describe, expect, it } from 'vitest'
import { missionListSchema, type Mission } from '../entities/mission/model'
import {
  checkApprovalReadiness,
  readCoverage,
  refusalMessage,
} from '../entities/mission/uiModel'
import type { PatrolArea } from '../entities/patrol-unit/model'
import { isInsideArea } from '../shared/geo/polygon'
import { readExample } from './publishedContract'

const patrolArea: PatrolArea = {
  id: '20000000-0000-0000-0000-000000000001',
  name: 'Primary Patrol Area',
  boundary: {
    type: 'Polygon',
    coordinates: [
      [
        [126.965, 37.558],
        [126.991, 37.558],
        [126.991, 37.575],
        [126.965, 37.575],
        [126.965, 37.558],
      ],
    ],
  },
}

function missionFromExample(status: Mission['status']): Mission {
  const list = missionListSchema.parse(readExample('patrol-mission-list'))
  const mission = list.missions.find((candidate) => candidate.status === status)
  if (mission === undefined) throw new Error(`no ${status} mission in the published example`)
  return mission
}

describe('readCoverage', () => {
  it('reports the three judgements separately and never folds the undecided ones', () => {
    const reading = readCoverage({ visited: 2, missed: 1, unknown: 1, planned: 1, total: 5 })

    expect(reading.visitedPercent).toBe(40)
    expect(reading.missedPercent).toBe(20)
    expect(reading.unknownPercent).toBe(20)
    // The three readings do not add up to 100 while checkpoints remain
    // unjudged, which is the honest picture.
    expect(reading.visitedPercent + reading.missedPercent + reading.unknownPercent).toBeLessThan(100)
    expect(reading.judged).toBe(3)
    expect(reading.label).toBe('통과 2 · 미순찰 1 · 판단 불가 1 / 5')
  })

  it('does not divide by zero for a plan without checkpoints', () => {
    const reading = readCoverage({ visited: 0, missed: 0, unknown: 0, planned: 0, total: 0 })

    expect(reading.visitedPercent).toBe(0)
    expect(reading.label).toBe('체크포인트 없음')
  })

  it('reads a published running mission without inventing misses', () => {
    const reading = readCoverage(missionFromExample('ACTIVE').coverage)

    expect(reading.missedPercent).toBe(0)
    expect(reading.unknownPercent).toBe(0)
  })
})

describe('checkApprovalReadiness', () => {
  it('names what a draft is still missing', () => {
    const draft = missionFromExample('DRAFT')

    const readiness = checkApprovalReadiness(draft, patrolArea)

    expect(readiness.ready).toBe(false)
    expect(readiness.problems).toContain('배정된 순찰체가 없습니다.')
  })

  it('names a checkpoint that falls outside the area', () => {
    const draft = missionFromExample('DRAFT')
    const outside: Mission = {
      ...draft,
      assignments: [
        { unitId: 'unit-1', callsign: 'SG-01', window: draft.window },
      ],
      checkpoints: [
        {
          ...draft.checkpoints[0],
          name: 'CP-구역밖',
          position: { latitude: 37.62, longitude: 127.08 },
        },
      ],
    }

    const readiness = checkApprovalReadiness(outside, patrolArea)

    expect(readiness.ready).toBe(false)
    expect(readiness.problems.some((problem) => problem.includes('CP-구역밖'))).toBe(true)
  })

  it('accepts a complete draft', () => {
    const draft = missionFromExample('DRAFT')
    const complete: Mission = {
      ...draft,
      assignments: [{ unitId: 'unit-1', callsign: 'SG-01', window: draft.window }],
    }

    expect(checkApprovalReadiness(complete, patrolArea).ready).toBe(true)
  })

  it('skips the containment check when the area geometry is not loaded', () => {
    const draft = missionFromExample('DRAFT')
    const complete: Mission = {
      ...draft,
      assignments: [{ unitId: 'unit-1', callsign: 'SG-01', window: draft.window }],
    }

    expect(checkApprovalReadiness(complete, null).ready).toBe(true)
  })
})

describe('refusalMessage', () => {
  it('explains a conflict with the units the server named', () => {
    expect(refusalMessage('ASSIGNMENT_CONFLICT', { callsigns: ['SG-01', 'SG-02'] })).toBe(
      'SG-01, SG-02가 겹치는 시간대에 이미 배정돼 있습니다.',
    )
  })

  it('explains a containment refusal with the checkpoint the server named', () => {
    expect(refusalMessage('CHECKPOINT_OUTSIDE_AREA', { checkpointName: 'CP-4' })).toBe(
      '순찰구역 밖 체크포인트: CP-4',
    )
  })

  it('explains a refused transition with the state it was refused in', () => {
    expect(refusalMessage('TRANSITION_NOT_ALLOWED', { status: 'READY' })).toBe(
      '승인됨 상태에서는 요청한 전이를 할 수 없습니다.',
    )
  })

  it('tells the operator to re-read after a stale version', () => {
    expect(refusalMessage('STALE_VERSION', { expectedVersion: 2, currentVersion: 3 })).toContain(
      '다시 시도',
    )
  })

  it('has wording for every published code', () => {
    const published = (readExample('api-error') as { code: string }).code
    expect(refusalMessage(published as 'ASSIGNMENT_CONFLICT')).not.toBe('')
    expect(refusalMessage('UNKNOWN')).toBe('요청이 거부됐습니다.')
  })
})

describe('isInsideArea', () => {
  it.each([
    [{ latitude: 37.5665, longitude: 126.978 }, true],
    [{ latitude: 37.561, longitude: 126.97 }, true],
    [{ latitude: 37.6, longitude: 127.05 }, false],
    [{ latitude: 37.55, longitude: 126.978 }, false],
  ])('judges %j as inside=%s', (point, expected) => {
    expect(isInsideArea(point, patrolArea)).toBe(expected)
  })
})
