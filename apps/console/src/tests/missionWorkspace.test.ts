import { describe, expect, it } from 'vitest'
import type { Mission } from '../entities/mission/model'
import { countCoverage, mockMissions, mockPatrolArea } from '../entities/mission/mockMissions'
import {
  initialWorkspace,
  missionWorkspaceReducer,
  type MissionWorkspaceAction,
  type MissionWorkspaceState,
} from '../pages/patrol-mission/missionWorkspace'

/**
 * The mock transitions are exercised here because the refusals they produce
 * are the ones the Go aggregate reproduces. Each case below is a rule from
 * docs/scenarios/patrol-mission.md.
 */

const ACTIVE_ID = '50000000-0000-0000-0000-000000000001'
const DRAFT_ID = '50000000-0000-0000-0000-000000000002'
const COMPLETED_ID = '50000000-0000-0000-0000-000000000003'

const now = new Date('2026-11-21T12:00:00Z')
const context = { area: mockPatrolArea, now }

function run(state: MissionWorkspaceState, ...actions: MissionWorkspaceAction[]) {
  return actions.reduce(
    (current, action) => missionWorkspaceReducer(current, action, context),
    state,
  )
}

function missionIn(state: MissionWorkspaceState, id: string): Mission {
  const mission = state.missions.find((candidate) => candidate.id === id)
  if (mission === undefined) throw new Error(`mission ${id} not found`)
  return mission
}

function stateWith(...missions: Mission[]): MissionWorkspaceState {
  return { missions, selectedId: missions[0]?.id ?? null, planner: null, notice: null }
}

function draftMission(overrides: Partial<Mission> = {}): Mission {
  const base = missionIn(initialWorkspace, DRAFT_ID)
  const merged: Mission = {
    ...base,
    assignments: [{ unitId: 'unit-1', callsign: 'SG-01', window: base.window }],
    ...overrides,
  }
  return { ...merged, coverage: countCoverage(merged.checkpoints) }
}

describe('approval preconditions', () => {
  it('refuses a draft with no checkpoints', () => {
    const next = run(stateWith(draftMission({ checkpoints: [] })), {
      type: 'transition',
      id: DRAFT_ID,
      transition: 'approve',
    })

    expect(missionIn(next, DRAFT_ID).status).toBe('DRAFT')
    expect(next.notice).toContain('체크포인트가 없습니다')
  })

  it('refuses a draft with no assigned unit', () => {
    const next = run(stateWith(draftMission({ assignments: [] })), {
      type: 'transition',
      id: DRAFT_ID,
      transition: 'approve',
    })

    expect(missionIn(next, DRAFT_ID).status).toBe('DRAFT')
    expect(next.notice).toContain('배정된 순찰체가 없습니다')
  })

  it('refuses a checkpoint outside the patrol area and names it', () => {
    const base = draftMission()
    const outside = draftMission({
      checkpoints: [
        base.checkpoints[0],
        {
          ...base.checkpoints[1],
          name: 'CP-구역밖',
          position: { latitude: 37.6, longitude: 127.05 },
        },
      ],
    })

    const next = run(stateWith(outside), {
      type: 'transition',
      id: DRAFT_ID,
      transition: 'approve',
    })

    expect(missionIn(next, DRAFT_ID).status).toBe('DRAFT')
    expect(next.notice).toContain('CP-구역밖')
  })

  it('approves a complete draft, bumps the version and pins the policy', () => {
    const draft = draftMission()

    const next = run(stateWith(draft), {
      type: 'transition',
      id: DRAFT_ID,
      transition: 'approve',
    })
    const mission = missionIn(next, DRAFT_ID)

    expect(mission.status).toBe('READY')
    expect(mission.version).toBe(draft.version + 1)
    expect(mission.approvedAt).toBe(now.toISOString())
    expect(mission.policyVersion).toBe(draft.policyVersion + 1)
    expect(mission.allowedTransitions).toEqual(['start', 'abort'])
  })

  it('refuses a unit already committed to an overlapping mission', () => {
    const draft = draftMission()
    const overlapping: Mission = {
      ...missionIn(initialWorkspace, ACTIVE_ID),
      window: draft.window,
      assignments: [{ unitId: 'unit-1', callsign: 'SG-01', window: draft.window }],
    }

    const next = run(stateWith(draft, overlapping), {
      type: 'transition',
      id: DRAFT_ID,
      transition: 'approve',
    })

    expect(missionIn(next, DRAFT_ID).status).toBe('DRAFT')
    expect(next.notice).toContain('SG-01')
  })
})

describe('transition guards', () => {
  it('refuses starting a mission that was never approved', () => {
    const next = run(stateWith(draftMission()), {
      type: 'transition',
      id: DRAFT_ID,
      transition: 'start',
    })

    expect(missionIn(next, DRAFT_ID).status).toBe('DRAFT')
    expect(next.notice).toContain('현재 상태에서는')
  })

  it('refuses any transition on a terminal mission', () => {
    const completed = missionIn(initialWorkspace, COMPLETED_ID)

    const next = run(stateWith(completed), {
      type: 'transition',
      id: COMPLETED_ID,
      transition: 'abort',
      reason: 'OPERATOR',
    })

    expect(missionIn(next, COMPLETED_ID).status).toBe('COMPLETED')
    expect(missionIn(next, COMPLETED_ID).version).toBe(completed.version)
  })

  it('refuses an abort without a reason', () => {
    const active = missionIn(initialWorkspace, ACTIVE_ID)

    const next = run(stateWith(active), {
      type: 'transition',
      id: ACTIVE_ID,
      transition: 'abort',
    })

    expect(missionIn(next, ACTIVE_ID).status).toBe('ACTIVE')
    expect(next.notice).toContain('중단 사유')
  })

  it('records the reason and the moment on an abort', () => {
    const active = missionIn(initialWorkspace, ACTIVE_ID)

    const next = run(stateWith(active), {
      type: 'transition',
      id: ACTIVE_ID,
      transition: 'abort',
      reason: 'BATTERY',
    })
    const mission = missionIn(next, ACTIVE_ID)

    expect(mission.status).toBe('ABORTED')
    expect(mission.abort).toEqual({ reason: 'BATTERY', at: now.toISOString() })
    expect(mission.endedAt).toBe(now.toISOString())
    expect(mission.allowedTransitions).toEqual([])
  })
})

describe('closing a mission', () => {
  it('leaves unjudged checkpoints undecided instead of calling them missed', () => {
    const active = missionIn(initialWorkspace, ACTIVE_ID)
    expect(active.coverage.planned).toBeGreaterThan(0)

    const next = run(stateWith(active), {
      type: 'transition',
      id: ACTIVE_ID,
      transition: 'complete',
    })
    const mission = missionIn(next, ACTIVE_ID)

    expect(mission.status).toBe('COMPLETED')
    expect(mission.coverage.planned).toBe(0)
    expect(mission.coverage.unknown).toBe(active.coverage.unknown + active.coverage.planned)
    expect(mission.coverage.visited).toBe(active.coverage.visited)
    expect(mission.coverage.missed).toBe(active.coverage.missed)
  })
})

describe('planning a mission', () => {
  it('places checkpoints in click order and can remove one', () => {
    const next = run(
      initialWorkspace,
      { type: 'planning/start' },
      { type: 'planning/placeCheckpoint', position: { latitude: 37.561, longitude: 126.97 } },
      { type: 'planning/placeCheckpoint', position: { latitude: 37.566, longitude: 126.985 } },
      { type: 'planning/placeCheckpoint', position: { latitude: 37.572, longitude: 126.987 } },
      { type: 'planning/removeCheckpoint', index: 1 },
    )

    expect(next.planner?.checkpoints.map((checkpoint) => checkpoint.name)).toEqual([
      'CP-1',
      'CP-3',
    ])
  })

  it('saves a draft that cannot run before approval', () => {
    const next = run(
      initialWorkspace,
      { type: 'planning/start' },
      { type: 'planning/name', name: '오후 순찰' },
      { type: 'planning/placeCheckpoint', position: { latitude: 37.561, longitude: 126.97 } },
      { type: 'planning/toggleUnit', callsign: 'SG-02' },
      { type: 'planning/create' },
    )

    const created = next.missions[0]
    expect(created.name).toBe('오후 순찰')
    expect(created.status).toBe('DRAFT')
    expect(created.version).toBe(0)
    expect(created.checkpoints[0].state).toBe('PLANNED')
    expect(created.assignments.map((assignment) => assignment.callsign)).toEqual(['SG-02'])
    expect(created.allowedTransitions).toEqual(['approve', 'abort'])
    expect(next.planner).toBeNull()
    expect(next.selectedId).toBe(created.id)
  })

  it('keeps the earlier missions when a draft is added', () => {
    const next = run(initialWorkspace, { type: 'planning/start' }, { type: 'planning/create' })

    expect(next.missions).toHaveLength(mockMissions.length + 1)
  })
})
