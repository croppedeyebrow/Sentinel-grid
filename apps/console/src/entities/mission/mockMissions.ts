import type { PatrolArea, PatrolRoute } from '../patrol-unit/model'
import type { Mission, MissionCheckpoint } from './model'

/**
 * Design-time mock for step 2 of the slice order. It exists to find the
 * screens and the domain questions before any mission API is written, and is
 * removed once the console talks to the real endpoints — the guide forbids
 * keeping a second implementation around after integration.
 *
 * The geometry matches the seeded city site so the mock screens line up with
 * the data the API will serve.
 */

export const mockPatrolArea: PatrolArea = {
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

export const mockPatrolRoute: PatrolRoute = {
  id: '40000000-0000-0000-0000-000000000001',
  patrolAreaId: mockPatrolArea.id,
  name: 'Riverside Loop',
  path: {
    type: 'LineString',
    coordinates: [
      [126.969, 37.56],
      [126.976, 37.562],
      [126.985, 37.566],
      [126.988, 37.572],
      [126.976, 37.5735],
      [126.969, 37.56],
    ],
  },
}

export const mockUnits = [
  { unitId: '30000000-0000-0000-0000-000000000001', callsign: 'SG-01' },
  { unitId: '30000000-0000-0000-0000-000000000002', callsign: 'SG-02' },
  { unitId: '30000000-0000-0000-0000-000000000005', callsign: 'SG-05' },
  { unitId: '30000000-0000-0000-0000-000000000006', callsign: 'SG-06' },
]

const checkpointPositions = [
  { name: 'CP-1 하천 진입', latitude: 37.561, longitude: 126.97 },
  { name: 'CP-2 보행교', latitude: 37.5625, longitude: 126.976 },
  { name: 'CP-3 공원 북측', latitude: 37.566, longitude: 126.985 },
  { name: 'CP-4 공사 구역', latitude: 37.572, longitude: 126.987 },
  { name: 'CP-5 하천 복귀', latitude: 37.573, longitude: 126.976 },
]

function checkpoints(
  states: MissionCheckpoint['state'][],
  creditedAt = '2026-11-21T09:12:00Z',
): MissionCheckpoint[] {
  return states.map((state, index) => ({
    id: `cp-${index + 1}`,
    sequence: index + 1,
    name: checkpointPositions[index].name,
    position: {
      latitude: checkpointPositions[index].latitude,
      longitude: checkpointPositions[index].longitude,
    },
    radiusMeters: 40,
    state,
    creditedObservedAt: state === 'VISITED' ? creditedAt : null,
  }))
}

export function countCoverage(list: readonly MissionCheckpoint[]): Mission['coverage'] {
  const count = (state: MissionCheckpoint['state']) =>
    list.filter((checkpoint) => checkpoint.state === state).length

  return {
    visited: count('VISITED'),
    missed: count('MISSED'),
    unknown: count('UNKNOWN'),
    planned: count('PLANNED'),
    total: list.length,
  }
}

function mission(
  partial: Omit<Mission, 'coverage' | 'patrolAreaId' | 'patrolRouteId'>,
): Mission {
  return {
    ...partial,
    patrolAreaId: mockPatrolArea.id,
    patrolRouteId: mockPatrolRoute.id,
    coverage: countCoverage(partial.checkpoints),
  }
}

export const mockMissions: Mission[] = [
  mission({
    id: 'mission-active',
    name: '하천변 정기 순찰 (오전)',
    version: 3,
    status: 'ACTIVE',
    windowStart: '2026-11-21T09:00:00Z',
    windowEnd: '2026-11-21T11:00:00Z',
    checkpoints: checkpoints(['VISITED', 'VISITED', 'UNKNOWN', 'MISSED', 'PLANNED']),
    assignments: [
      {
        unitId: mockUnits[0].unitId,
        callsign: 'SG-01',
        windowStart: '2026-11-21T09:00:00Z',
        windowEnd: '2026-11-21T11:00:00Z',
      },
    ],
    abort: null,
    createdAt: '2026-11-21T08:30:00Z',
    approvedAt: '2026-11-21T08:50:00Z',
    startedAt: '2026-11-21T09:00:00Z',
    endedAt: null,
  }),
  mission({
    id: 'mission-draft',
    name: '공사 구역 상황 순찰',
    version: 0,
    status: 'DRAFT',
    windowStart: '2026-11-21T13:00:00Z',
    windowEnd: '2026-11-21T14:00:00Z',
    checkpoints: checkpoints(['PLANNED', 'PLANNED']),
    assignments: [],
    abort: null,
    createdAt: '2026-11-21T10:05:00Z',
    approvedAt: null,
    startedAt: null,
    endedAt: null,
  }),
  mission({
    id: 'mission-completed',
    name: '하천변 정기 순찰 (야간)',
    version: 5,
    status: 'COMPLETED',
    windowStart: '2026-11-20T21:00:00Z',
    windowEnd: '2026-11-20T23:00:00Z',
    checkpoints: checkpoints(
      ['VISITED', 'VISITED', 'VISITED', 'UNKNOWN', 'VISITED'],
      '2026-11-20T22:10:00Z',
    ),
    assignments: [
      {
        unitId: mockUnits[2].unitId,
        callsign: 'SG-05',
        windowStart: '2026-11-20T21:00:00Z',
        windowEnd: '2026-11-20T23:00:00Z',
      },
    ],
    abort: null,
    createdAt: '2026-11-20T20:30:00Z',
    approvedAt: '2026-11-20T20:45:00Z',
    startedAt: '2026-11-20T21:00:00Z',
    endedAt: '2026-11-20T23:02:00Z',
  }),
  mission({
    id: 'mission-aborted',
    name: '외곽 점검 순찰',
    version: 4,
    status: 'ABORTED',
    windowStart: '2026-11-20T15:00:00Z',
    windowEnd: '2026-11-20T17:00:00Z',
    checkpoints: checkpoints(['VISITED', 'MISSED', 'UNKNOWN', 'UNKNOWN', 'UNKNOWN']),
    assignments: [
      {
        unitId: mockUnits[3].unitId,
        callsign: 'SG-06',
        windowStart: '2026-11-20T15:00:00Z',
        windowEnd: '2026-11-20T17:00:00Z',
      },
    ],
    abort: { reason: 'BATTERY', at: '2026-11-20T15:40:00Z' },
    createdAt: '2026-11-20T14:30:00Z',
    approvedAt: '2026-11-20T14:50:00Z',
    startedAt: '2026-11-20T15:00:00Z',
    endedAt: '2026-11-20T15:40:00Z',
  }),
]
