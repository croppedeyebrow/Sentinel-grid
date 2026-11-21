import { describe, expect, it } from 'vitest'
import type { Mission } from '../entities/mission/model'
import { countCoverage, mockPatrolArea, mockMissions } from '../entities/mission/mockMissions'
import {
  initialWorkspace,
  missionWorkspaceReducer,
  type MissionWorkspaceAction,
  type MissionWorkspaceState,
} from '../pages/patrol-mission/missionWorkspace'

/**
 * The mock transitions are exercised here because the refusals they produce
 * are the ones the Go aggregate has to reproduce in step 3. Each case below is
 * a rule from docs/scenarios/patrol-mission.md.
 */

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

function stateWith(mission: Mission): MissionWorkspaceState {
  return { missions: [mission], selectedId: mission.id, planner: null, notice: null }
}

function draftMission(overrides: Partial<Mission> = {}): Mission {
  const base = missionIn(initialWorkspace, 'mission-draft')
  const merged: Mission = {
    ...base,
    assignments: [
      {
        unitId: 'unit-1',
        callsign: 'SG-01',
        windowStart: base.windowStart,
        windowEnd: base.windowEnd,
      },
    ],
    ...overrides,
  }
  return { ...merged, coverage: countCoverage(merged.checkpoints) }
}

describe('approval preconditions', () => {
  it('refuses a draft with no checkpoints', () => {
    const state = stateWith(draftMission({ checkpoints: [] }))

    const next = run(state, { type: 'transition', id: 'mission-draft', transition: 'approve' })

    expect(missionIn(next, 'mission-draft').status).toBe('DRAFT')
    expect(next.notice).toContain('체크포인트가 없습니다')
  })

  it('refuses a draft with no assigned unit', () => {
    const state = stateWith(draftMission({ assignments: [] }))

    const next = run(state, { type: 'transition', id: 'mission-draft', transition: 'approve' })

    expect(missionIn(next, 'mission-draft').status).toBe('DRAFT')
    expect(next.notice).toContain('배정된 순찰체가 없습니다')
  })

  it('refuses a checkpoint outside the patrol area and names it', () => {
    const base = draftMission()
    const outside: Mission = {
      ...base,
      checkpoints: [
        base.checkpoints[0],
        {
          ...base.checkpoints[1],
          name: 'CP-구역밖',
          position: { latitude: 37.6, longitude: 127.05 },
        },
      ],
    }

    const next = run(stateWith(outside), {
      type: 'transition',
      id: 'mission-draft',
      transition: 'approve',
    })

    expect(missionIn(next, 'mission-draft').status).toBe('DRAFT')
    expect(next.notice).toContain('CP-구역밖')
  })

  it('approves a complete draft and bumps the version', () => {
    const state = stateWith(draftMission())

    const next = run(state, { type: 'transition', id: 'mission-draft', transition: 'approve' })
    const mission = missionIn(next, 'mission-draft')

    expect(mission.status).toBe('READY')
    expect(mission.version).toBe(1)
    expect(mission.approvedAt).toBe(now.toISOString())
  })

  it('refuses a unit already committed to an overlapping mission', () => {
    const draft = draftMission()
    const overlapping = missionIn(initialWorkspace, 'mission-active')
    const state: MissionWorkspaceState = {
      missions: [
        draft,
        {
          ...overlapping,
          windowStart: draft.windowStart,
          windowEnd: draft.windowEnd,
          assignments: [
            {
              unitId: 'unit-1',
              callsign: 'SG-01',
              windowStart: draft.windowStart,
              windowEnd: draft.windowEnd,
            },
          ],
        },
      ],
      selectedId: draft.id,
      planner: null,
      notice: null,
    }

    const next = run(state, { type: 'transition', id: 'mission-draft', transition: 'approve' })

    expect(missionIn(next, 'mission-draft').status).toBe('DRAFT')
    expect(next.notice).toContain('SG-01')
  })
})

describe('transition guards', () => {
  it('refuses starting a mission that was never approved', () => {
    const state = stateWith(draftMission())

    const next = run(state, { type: 'transition', id: 'mission-draft', transition: 'start' })

    expect(missionIn(next, 'mission-draft').status).toBe('DRAFT')
    expect(next.notice).toContain('현재 상태에서는')
  })

  it('refuses any transition on a terminal mission', () => {
    const completed = missionIn(initialWorkspace, 'mission-completed')

    const next = run(stateWith(completed), {
      type: 'transition',
      id: completed.id,
      transition: 'abort',
      reason: 'OPERATOR',
    })

    expect(missionIn(next, completed.id).status).toBe('COMPLETED')
    expect(missionIn(next, completed.id).version).toBe(completed.version)
  })

  it('refuses an abort without a reason', () => {
    const active = missionIn(initialWorkspace, 'mission-active')

    const next = run(stateWith(active), {
      type: 'transition',
      id: active.id,
      transition: 'abort',
    })

    expect(missionIn(next, active.id).status).toBe('ACTIVE')
    expect(next.notice).toContain('중단 사유')
  })

  it('records the reason and the moment on an abort', () => {
    const active = missionIn(initialWorkspace, 'mission-active')

    const next = run(stateWith(active), {
      type: 'transition',
      id: active.id,
      transition: 'abort',
      reason: 'BATTERY',
    })
    const mission = missionIn(next, active.id)

    expect(mission.status).toBe('ABORTED')
    expect(mission.abort).toEqual({ reason: 'BATTERY', at: now.toISOString() })
    expect(mission.endedAt).toBe(now.toISOString())
  })
})

describe('closing a mission', () => {
  it('leaves unjudged checkpoints undecided instead of calling them missed', () => {
    const active = missionIn(initialWorkspace, 'mission-active')
    expect(active.coverage.planned).toBeGreaterThan(0)

    const next = run(stateWith(active), {
      type: 'transition',
      id: active.id,
      transition: 'complete',
    })
    const mission = missionIn(next, active.id)

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
    expect(next.planner).toBeNull()
    expect(next.selectedId).toBe(created.id)
  })

  it('keeps the earlier missions when a draft is added', () => {
    const next = run(initialWorkspace, { type: 'planning/start' }, { type: 'planning/create' })

    expect(next.missions).toHaveLength(mockMissions.length + 1)
  })
})
