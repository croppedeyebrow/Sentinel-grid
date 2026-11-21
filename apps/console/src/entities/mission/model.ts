import { z } from 'zod'
import { positionSchema } from '../patrol-unit/model'

/**
 * Shape of a patrol mission as the console needs it.
 *
 * This is the design-time draft from step 2 of the slice order: the mock UI is
 * built against these types so the HTTP contract in `contracts/` can be
 * written from what the screen actually needs. Mission state and checkpoint
 * judgement stay separate fields, the same way condition and freshness do in
 * the Operational Picture.
 */

export const missionStatusSchema = z.enum(['DRAFT', 'READY', 'ACTIVE', 'COMPLETED', 'ABORTED'])

/**
 * `PLANNED` is not a coverage verdict: it means the mission has not run yet, so
 * no judgement exists. The three judgements are the ones fixed in the
 * scenario: visited, missed, or undecided for lack of trustworthy evidence.
 */
export const checkpointStateSchema = z.enum(['PLANNED', 'VISITED', 'MISSED', 'UNKNOWN'])

export const abortReasonSchema = z.enum(['OPERATOR', 'BATTERY', 'LINK', 'SAFETY', 'POLICY'])

const isoDateTime = z.string().datetime({ offset: true })

export const missionCheckpointSchema = z.object({
  id: z.string().min(1),
  sequence: z.number().int().positive(),
  name: z.string().min(1),
  position: positionSchema,
  radiusMeters: z.number().positive(),
  state: checkpointStateSchema,
  /** Observation that credited the pass, present only for VISITED. */
  creditedObservedAt: isoDateTime.nullable(),
})

export const missionAssignmentSchema = z.object({
  unitId: z.string().min(1),
  callsign: z.string().min(1),
  windowStart: isoDateTime,
  windowEnd: isoDateTime,
})

export const missionCoverageSchema = z.object({
  visited: z.number().int().min(0),
  missed: z.number().int().min(0),
  unknown: z.number().int().min(0),
  planned: z.number().int().min(0),
  total: z.number().int().min(0),
})

export const missionAbortSchema = z.object({
  reason: abortReasonSchema,
  at: isoDateTime,
})

export const missionSchema = z.object({
  id: z.string().min(1),
  name: z.string().min(1),
  /** Every transition must carry the version the caller last saw. */
  version: z.number().int().nonnegative(),
  status: missionStatusSchema,
  patrolAreaId: z.string().min(1),
  patrolRouteId: z.string().min(1).nullable(),
  windowStart: isoDateTime,
  windowEnd: isoDateTime,
  checkpoints: z.array(missionCheckpointSchema),
  assignments: z.array(missionAssignmentSchema),
  coverage: missionCoverageSchema,
  abort: missionAbortSchema.nullable(),
  createdAt: isoDateTime,
  approvedAt: isoDateTime.nullable(),
  startedAt: isoDateTime.nullable(),
  endedAt: isoDateTime.nullable(),
})

export type MissionStatus = z.infer<typeof missionStatusSchema>
export type CheckpointState = z.infer<typeof checkpointStateSchema>
export type AbortReason = z.infer<typeof abortReasonSchema>
export type MissionCheckpoint = z.infer<typeof missionCheckpointSchema>
export type MissionAssignment = z.infer<typeof missionAssignmentSchema>
export type MissionCoverage = z.infer<typeof missionCoverageSchema>
export type Mission = z.infer<typeof missionSchema>
