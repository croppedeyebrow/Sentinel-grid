import type { PatrolArea, Position } from '../../entities/patrol-unit/model'
import type { AbortReason, Mission, MissionCheckpoint } from '../../entities/mission/model'
import { countCoverage, mockMissions, mockUnits } from '../../entities/mission/mockMissions'
import {
  allowedTransitions,
  checkApprovalReadiness,
  conflictingUnits,
  type MissionTransition,
} from '../../entities/mission/uiModel'

/**
 * Local workspace state for the mock mission screens.
 *
 * The transitions here stand in for the Go aggregate while the UI draft is
 * being explored. Rejections are modelled too, because the point of the draft
 * is to find out which refusals the operator has to understand — those become
 * the domain tests in step 3.
 */

export interface PlannerDraft {
  name: string
  windowStart: string
  windowEnd: string
  checkpoints: { name: string; position: Position }[]
  assignedCallsigns: string[]
}

export interface MissionWorkspaceState {
  missions: Mission[]
  selectedId: string | null
  planner: PlannerDraft | null
  /** Last refusal or confirmation shown to the operator. */
  notice: string | null
}

export type MissionWorkspaceAction =
  | { type: 'select'; id: string }
  | { type: 'planning/start' }
  | { type: 'planning/cancel' }
  | { type: 'planning/name'; name: string }
  | { type: 'planning/window'; windowStart: string; windowEnd: string }
  | { type: 'planning/placeCheckpoint'; position: Position }
  | { type: 'planning/removeCheckpoint'; index: number }
  | { type: 'planning/toggleUnit'; callsign: string }
  | { type: 'planning/create' }
  | { type: 'transition'; id: string; transition: MissionTransition; reason?: AbortReason }

export const initialWorkspace: MissionWorkspaceState = {
  missions: mockMissions,
  selectedId: mockMissions[0]?.id ?? null,
  planner: null,
  notice: null,
}

export function emptyPlanner(now: Date): PlannerDraft {
  const start = new Date(now.getTime() + 30 * 60 * 1000)
  const end = new Date(start.getTime() + 2 * 60 * 60 * 1000)
  return {
    name: '',
    windowStart: start.toISOString(),
    windowEnd: end.toISOString(),
    checkpoints: [],
    assignedCallsigns: [],
  }
}

function draftToMission(draft: PlannerDraft, now: Date, sequence: number): Mission {
  const checkpoints: MissionCheckpoint[] = draft.checkpoints.map((checkpoint, index) => ({
    id: `cp-new-${index + 1}`,
    sequence: index + 1,
    name: checkpoint.name,
    position: checkpoint.position,
    radiusMeters: 40,
    state: 'PLANNED',
    creditedObservedAt: null,
  }))

  return {
    id: `mission-new-${sequence}`,
    name: draft.name.trim() === '' ? '이름 없는 임무' : draft.name.trim(),
    version: 0,
    status: 'DRAFT',
    patrolAreaId: mockMissions[0].patrolAreaId,
    patrolRouteId: mockMissions[0].patrolRouteId,
    windowStart: draft.windowStart,
    windowEnd: draft.windowEnd,
    checkpoints,
    assignments: draft.assignedCallsigns.map((callsign) => ({
      unitId: mockUnits.find((unit) => unit.callsign === callsign)?.unitId ?? callsign,
      callsign,
      windowStart: draft.windowStart,
      windowEnd: draft.windowEnd,
    })),
    coverage: countCoverage(checkpoints),
    abort: null,
    createdAt: now.toISOString(),
    approvedAt: null,
    startedAt: null,
    endedAt: null,
  }
}

/**
 * On a terminal transition the undecided checkpoints become UNKNOWN rather
 * than MISSED. The mock has no observation history, and claiming a checkpoint
 * was missed without evidence would invent a failure — the server decides this
 * from observation quality.
 */
function closeCheckpoints(checkpoints: readonly MissionCheckpoint[]): MissionCheckpoint[] {
  return checkpoints.map((checkpoint) =>
    checkpoint.state === 'PLANNED' ? { ...checkpoint, state: 'UNKNOWN' as const } : checkpoint,
  )
}

interface TransitionOutcome {
  mission: Mission
  notice: string | null
}

function applyTransition(
  mission: Mission,
  action: Extract<MissionWorkspaceAction, { type: 'transition' }>,
  missions: readonly Mission[],
  area: PatrolArea,
  now: Date,
): TransitionOutcome {
  if (!allowedTransitions(mission.status).includes(action.transition)) {
    return {
      mission,
      notice: `${mission.name}: 현재 상태에서는 요청한 전이를 할 수 없습니다.`,
    }
  }

  const next = { ...mission, version: mission.version + 1 }

  if (action.transition === 'approve') {
    const readiness = checkApprovalReadiness(mission, area)
    if (!readiness.ready) {
      return { mission, notice: `승인 거부 — ${readiness.problems.join(' ')}` }
    }
    const conflicts = conflictingUnits(missions, mission)
    if (conflicts.length > 0) {
      return {
        mission,
        notice: `승인 거부 — ${conflicts.join(', ')}가 겹치는 시간대에 이미 배정돼 있습니다.`,
      }
    }
    return {
      mission: { ...next, status: 'READY', approvedAt: now.toISOString() },
      notice: `${mission.name}을 승인했습니다. 구역·경로·체크포인트·정책이 이 임무 버전에 고정됩니다.`,
    }
  }

  if (action.transition === 'start') {
    const conflicts = conflictingUnits(missions, mission)
    if (conflicts.length > 0) {
      return {
        mission,
        notice: `시작 거부 — ${conflicts.join(', ')}가 다른 임무에서 진행 중입니다.`,
      }
    }
    return {
      mission: { ...next, status: 'ACTIVE', startedAt: now.toISOString() },
      notice: null,
    }
  }

  if (action.transition === 'complete') {
    const checkpoints = closeCheckpoints(mission.checkpoints)
    return {
      mission: {
        ...next,
        status: 'COMPLETED',
        endedAt: now.toISOString(),
        checkpoints,
        coverage: countCoverage(checkpoints),
      },
      notice: null,
    }
  }

  if (action.reason === undefined) {
    return { mission, notice: '중단 거부 — 중단 사유를 선택해야 합니다.' }
  }
  const checkpoints = closeCheckpoints(mission.checkpoints)
  return {
    mission: {
      ...next,
      status: 'ABORTED',
      endedAt: now.toISOString(),
      abort: { reason: action.reason, at: now.toISOString() },
      checkpoints,
      coverage: countCoverage(checkpoints),
    },
    notice: null,
  }
}

export function missionWorkspaceReducer(
  state: MissionWorkspaceState,
  action: MissionWorkspaceAction,
  context: { area: PatrolArea; now: Date },
): MissionWorkspaceState {
  switch (action.type) {
    case 'select':
      return { ...state, selectedId: action.id, planner: null, notice: null }

    case 'planning/start':
      return { ...state, planner: emptyPlanner(context.now), selectedId: null, notice: null }

    case 'planning/cancel':
      return { ...state, planner: null, notice: null }

    case 'planning/name':
      return state.planner === null
        ? state
        : { ...state, planner: { ...state.planner, name: action.name } }

    case 'planning/window':
      return state.planner === null
        ? state
        : {
            ...state,
            planner: {
              ...state.planner,
              windowStart: action.windowStart,
              windowEnd: action.windowEnd,
            },
          }

    case 'planning/placeCheckpoint': {
      if (state.planner === null) return state
      const sequence = state.planner.checkpoints.length + 1
      return {
        ...state,
        planner: {
          ...state.planner,
          checkpoints: [
            ...state.planner.checkpoints,
            { name: `CP-${sequence}`, position: action.position },
          ],
        },
        notice: null,
      }
    }

    case 'planning/removeCheckpoint': {
      if (state.planner === null) return state
      return {
        ...state,
        planner: {
          ...state.planner,
          checkpoints: state.planner.checkpoints.filter((_, index) => index !== action.index),
        },
      }
    }

    case 'planning/toggleUnit': {
      if (state.planner === null) return state
      const assigned = state.planner.assignedCallsigns.includes(action.callsign)
      return {
        ...state,
        planner: {
          ...state.planner,
          assignedCallsigns: assigned
            ? state.planner.assignedCallsigns.filter((callsign) => callsign !== action.callsign)
            : [...state.planner.assignedCallsigns, action.callsign],
        },
      }
    }

    case 'planning/create': {
      if (state.planner === null) return state
      const created = draftToMission(state.planner, context.now, state.missions.length + 1)
      return {
        missions: [created, ...state.missions],
        selectedId: created.id,
        planner: null,
        notice: `${created.name}을 초안으로 저장했습니다. 승인 전에는 실행할 수 없습니다.`,
      }
    }

    case 'transition': {
      const target = state.missions.find((mission) => mission.id === action.id)
      if (target === undefined) return state

      const outcome = applyTransition(target, action, state.missions, context.area, context.now)
      return {
        ...state,
        missions: state.missions.map((mission) =>
          mission.id === action.id ? outcome.mission : mission,
        ),
        notice: outcome.notice,
      }
    }
  }
}
