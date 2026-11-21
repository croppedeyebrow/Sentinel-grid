import {
  missionListSchema,
  missionSchema,
  type AbortReason,
  type Mission,
  type MissionTransition,
} from '../../entities/mission/model'
import type { Position } from '../../entities/patrol-unit/model'
import { API_BASE_URL } from './config'
import { refusalFrom } from './apiError'

/**
 * Patrol Mission endpoints, as published in contracts/README.md. Every
 * response is validated with the Zod schemas before it reaches the UI, and
 * every refusal is turned into an ApiRefusal carrying the contract's code.
 */

async function send(path: string, init: RequestInit = {}): Promise<unknown> {
  const response = await fetch(`${API_BASE_URL}${path}`, {
    ...init,
    headers: {
      ...(init.body === undefined ? {} : { 'Content-Type': 'application/json' }),
      ...init.headers,
    },
  })
  if (!response.ok) throw await refusalFrom(response)
  return response.json()
}

export async function getMissions(): Promise<Mission[]> {
  return missionListSchema.parse(await send('/missions')).missions
}

export interface NewCheckpointInput {
  name: string
  position: Position
  radiusMeters: number
}

export interface CreateMissionInput {
  name: string
  patrolAreaId: string
  patrolRouteId?: string | null
  window: { start: string; end: string }
  checkpoints: NewCheckpointInput[]
  unitIds: string[]
}

/**
 * Creates a draft. The idempotency key is generated per attempt so a retried
 * request cannot plan the mission twice.
 */
export async function createMission(input: CreateMissionInput): Promise<Mission> {
  const body = {
    name: input.name,
    patrolAreaId: input.patrolAreaId,
    patrolRouteId: input.patrolRouteId ?? null,
    window: input.window,
    checkpoints: input.checkpoints,
    assignments: input.unitIds.map((unitId) => ({ unitId })),
  }

  return missionSchema.parse(
    await send('/missions', {
      method: 'POST',
      headers: { 'Idempotency-Key': crypto.randomUUID() },
      body: JSON.stringify(body),
    }),
  )
}

export async function addCheckpoint(
  missionId: string,
  checkpoint: NewCheckpointInput,
): Promise<Mission> {
  return missionSchema.parse(
    await send(`/missions/${missionId}/checkpoints`, {
      method: 'POST',
      body: JSON.stringify(checkpoint),
    }),
  )
}

export async function removeCheckpoint(
  missionId: string,
  checkpointId: string,
): Promise<Mission> {
  return missionSchema.parse(
    await send(`/missions/${missionId}/checkpoints/${checkpointId}`, { method: 'DELETE' }),
  )
}

export async function assignUnit(missionId: string, unitId: string): Promise<Mission> {
  return missionSchema.parse(
    await send(`/missions/${missionId}/assignments`, {
      method: 'POST',
      body: JSON.stringify({ unitId }),
    }),
  )
}

export async function unassignUnit(missionId: string, unitId: string): Promise<Mission> {
  return missionSchema.parse(
    await send(`/missions/${missionId}/assignments/${unitId}`, { method: 'DELETE' }),
  )
}

export interface TransitionInput {
  missionId: string
  transition: MissionTransition
  /** The version the operator last saw; a stale one is refused by the server. */
  version: number
  reason?: AbortReason
}

export async function transitionMission(input: TransitionInput): Promise<Mission> {
  const body: { version: number; reason?: AbortReason } = { version: input.version }
  if (input.reason !== undefined) body.reason = input.reason

  return missionSchema.parse(
    await send(`/missions/${input.missionId}/${input.transition}`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  )
}
