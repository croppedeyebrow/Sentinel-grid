import type { PatrolArea, PatrolRoute } from '../patrol-unit/model'
import type { CheckpointState, Mission, MissionCheckpoint, NamedReference } from './model'
import { allowedTransitions } from './uiModel'

/**
 * Design-time mock for the mission screens. It follows
 * `contracts/schemas/patrol-mission.schema.json` field for field, so swapping
 * it for the real endpoints in step 6 is a change of data source and nothing
 * else. The guide forbids keeping a second implementation after integration,
 * so this file goes away then.
 *
 * Checkpoint states obey the domain rules: a running mission has no missed or
 * undecided checkpoints, because the unit may still get there.
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

const areaReference: NamedReference = { id: mockPatrolArea.id, name: mockPatrolArea.name }
const routeReference: NamedReference = { id: mockPatrolRoute.id, name: mockPatrolRoute.name }

export const mockUnits = [
  { unitId: '30000000-0000-0000-0000-000000000001', callsign: 'SG-01' },
  { unitId: '30000000-0000-0000-0000-000000000002', callsign: 'SG-02' },
  { unitId: '30000000-0000-0000-0000-000000000005', callsign: 'SG-05' },
  { unitId: '30000000-0000-0000-0000-000000000006', callsign: 'SG-06' },
]

const checkpointPlan = [
  { name: 'CP-1 하천 진입', latitude: 37.561, longitude: 126.97 },
  { name: 'CP-2 보행교', latitude: 37.5625, longitude: 126.976 },
  { name: 'CP-3 공원 북측', latitude: 37.566, longitude: 126.985 },
  { name: 'CP-4 공사 구역', latitude: 37.572, longitude: 126.987 },
  { name: 'CP-5 하천 복귀', latitude: 37.573, longitude: 126.976 },
]

function checkpoints(
  prefix: string,
  states: CheckpointState[],
  creditedAt = '2026-11-21T09:12:00Z',
): MissionCheckpoint[] {
  return states.map((state, index) => ({
    id: `${prefix}-cp-${index + 1}`,
    sequence: index + 1,
    name: checkpointPlan[index].name,
    position: {
      latitude: checkpointPlan[index].latitude,
      longitude: checkpointPlan[index].longitude,
    },
    radiusMeters: 40,
    state,
    creditedObservedAt: state === 'VISITED' ? creditedAt : null,
  }))
}

export function countCoverage(list: readonly MissionCheckpoint[]): Mission['coverage'] {
  const count = (state: CheckpointState) =>
    list.filter((checkpoint) => checkpoint.state === state).length

  return {
    visited: count('VISITED'),
    missed: count('MISSED'),
    unknown: count('UNKNOWN'),
    planned: count('PLANNED'),
    total: list.length,
  }
}

type MissionDraft = Omit<Mission, 'coverage' | 'allowedTransitions' | 'patrolArea'> & {
  patrolArea?: NamedReference
}

function mission(partial: MissionDraft): Mission {
  return {
    ...partial,
    patrolArea: partial.patrolArea ?? areaReference,
    coverage: countCoverage(partial.checkpoints),
    allowedTransitions: allowedTransitions(partial.status),
  }
}

export const mockMissions: Mission[] = [
  mission({
    id: '50000000-0000-0000-0000-000000000001',
    name: '하천변 정기 순찰 (오전)',
    version: 3,
    status: 'ACTIVE',
    patrolRoute: routeReference,
    window: { start: '2026-11-21T09:00:00Z', end: '2026-11-21T11:00:00Z' },
    policyVersion: 1,
    checkpoints: checkpoints('active', [
      'VISITED',
      'VISITED',
      'PLANNED',
      'PLANNED',
      'PLANNED',
    ]),
    assignments: [
      {
        unitId: mockUnits[0].unitId,
        callsign: 'SG-01',
        window: { start: '2026-11-21T09:00:00Z', end: '2026-11-21T11:00:00Z' },
      },
    ],
    abort: null,
    createdAt: '2026-11-21T08:30:00Z',
    approvedAt: '2026-11-21T08:50:00Z',
    startedAt: '2026-11-21T09:00:00Z',
    endedAt: null,
  }),
  mission({
    id: '50000000-0000-0000-0000-000000000002',
    name: '공사 구역 상황 순찰',
    version: 0,
    status: 'DRAFT',
    patrolRoute: null,
    window: { start: '2026-11-21T13:00:00Z', end: '2026-11-21T14:00:00Z' },
    policyVersion: 0,
    checkpoints: checkpoints('draft', ['PLANNED', 'PLANNED']),
    assignments: [],
    abort: null,
    createdAt: '2026-11-21T10:05:00Z',
    approvedAt: null,
    startedAt: null,
    endedAt: null,
  }),
  mission({
    id: '50000000-0000-0000-0000-000000000003',
    name: '하천변 정기 순찰 (야간)',
    version: 5,
    status: 'COMPLETED',
    patrolRoute: routeReference,
    window: { start: '2026-11-20T21:00:00Z', end: '2026-11-20T23:00:00Z' },
    policyVersion: 1,
    checkpoints: checkpoints(
      'completed',
      ['VISITED', 'VISITED', 'MISSED', 'VISITED'],
      '2026-11-20T22:10:00Z',
    ),
    assignments: [
      {
        unitId: mockUnits[2].unitId,
        callsign: 'SG-05',
        window: { start: '2026-11-20T21:00:00Z', end: '2026-11-20T23:00:00Z' },
      },
    ],
    abort: null,
    createdAt: '2026-11-20T20:30:00Z',
    approvedAt: '2026-11-20T20:45:00Z',
    startedAt: '2026-11-20T21:00:00Z',
    endedAt: '2026-11-20T23:02:00Z',
  }),
  mission({
    id: '50000000-0000-0000-0000-000000000004',
    name: '외곽 점검 순찰 (통신 단절)',
    version: 4,
    status: 'ABORTED',
    patrolRoute: null,
    window: { start: '2026-11-20T15:00:00Z', end: '2026-11-20T17:00:00Z' },
    policyVersion: 1,
    checkpoints: checkpoints('aborted', ['UNKNOWN', 'UNKNOWN', 'UNKNOWN']),
    assignments: [
      {
        unitId: mockUnits[3].unitId,
        callsign: 'SG-06',
        window: { start: '2026-11-20T15:00:00Z', end: '2026-11-20T17:00:00Z' },
      },
    ],
    abort: { reason: 'LINK', at: '2026-11-20T15:40:00Z' },
    createdAt: '2026-11-20T14:30:00Z',
    approvedAt: '2026-11-20T14:50:00Z',
    startedAt: '2026-11-20T15:00:00Z',
    endedAt: '2026-11-20T15:40:00Z',
  }),
]
