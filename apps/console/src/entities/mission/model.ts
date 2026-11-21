import { z } from 'zod'
import { positionSchema } from '../patrol-unit/model'

/**
 * Runtime contract for the Patrol Mission endpoints, mirroring
 * `contracts/schemas/patrol-mission*.schema.json`. The published examples are
 * parsed with these schemas in the tests, so the two representations cannot
 * drift apart.
 *
 * Mission `status` and checkpoint `state` answer different questions and stay
 * separate fields, the same way condition and freshness do for a patrol unit.
 */

export const missionStatusSchema = z.enum(['DRAFT', 'READY', 'ACTIVE', 'COMPLETED', 'ABORTED'])
export const missionTransitionSchema = z.enum(['approve', 'start', 'complete', 'abort'])

/**
 * `PLANNED` is not a coverage verdict: the mission has not reached the
 * checkpoint, so nothing is claimed about it.
 */
export const checkpointStateSchema = z.enum(['PLANNED', 'VISITED', 'MISSED', 'UNKNOWN'])

export const abortReasonSchema = z.enum(['OPERATOR', 'BATTERY', 'LINK', 'SAFETY', 'POLICY'])

const isoDateTime = z.string().datetime({ offset: true })

export const namedReferenceSchema = z.object({
  id: z.string().min(1),
  name: z.string().min(1),
})

export const windowSchema = z.object({
  start: isoDateTime,
  end: isoDateTime,
})

export const missionCheckpointSchema = z.object({
  id: z.string().min(1),
  sequence: z.number().int().positive(),
  name: z.string().min(1),
  position: positionSchema,
  radiusMeters: z.number().positive(),
  state: checkpointStateSchema,
  /** Observation that credited the pass; present only for VISITED. */
  creditedObservedAt: isoDateTime.nullable(),
})

export const missionAssignmentSchema = z.object({
  unitId: z.string().min(1),
  callsign: z.string().min(1),
  window: windowSchema,
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
  /** Revision to echo back on a transition; a stale one is refused. */
  version: z.number().int().min(0),
  status: missionStatusSchema,
  patrolArea: namedReferenceSchema,
  patrolRoute: namedReferenceSchema.nullable(),
  window: windowSchema,
  /** Site Profile revision pinned at approval. */
  policyVersion: z.number().int().min(0),
  checkpoints: z.array(missionCheckpointSchema),
  assignments: z.array(missionAssignmentSchema),
  coverage: missionCoverageSchema,
  abort: missionAbortSchema.nullable(),
  /** Transitions the server will accept; the console offers these, not its own. */
  allowedTransitions: z.array(missionTransitionSchema),
  createdAt: isoDateTime,
  approvedAt: isoDateTime.nullable(),
  startedAt: isoDateTime.nullable(),
  endedAt: isoDateTime.nullable(),
})

export const missionListSchema = z.object({
  generatedAt: isoDateTime,
  missions: z.array(missionSchema),
})

export const createMissionRequestSchema = z.object({
  name: z.string().min(1).max(200),
  patrolAreaId: z.string().min(1),
  patrolRouteId: z.string().min(1).nullable().optional(),
  window: windowSchema,
  checkpoints: z.array(
    z.object({
      name: z.string().min(1).max(200),
      position: positionSchema,
      radiusMeters: z.number().positive().max(1000),
    }),
  ),
  assignments: z.array(z.object({ unitId: z.string().min(1) })).optional(),
})

export const transitionRequestSchema = z.object({
  version: z.number().int().min(0),
  /** Required by the abort endpoint, ignored by the others. */
  reason: abortReasonSchema.optional(),
})

export const errorCodeSchema = z.enum([
  'INVALID_REQUEST',
  'MISSION_NOT_FOUND',
  'PATROL_AREA_NOT_FOUND',
  'UNIT_NOT_FOUND',
  'NO_CHECKPOINTS',
  'NO_ASSIGNMENT',
  'INVALID_WINDOW',
  'CHECKPOINT_OUTSIDE_AREA',
  'ASSIGNMENT_CONFLICT',
  'TRANSITION_NOT_ALLOWED',
  'STALE_VERSION',
  'ABORT_REASON_REQUIRED',
  'UNKNOWN_ABORT_REASON',
  'NOT_DRAFT',
  'NOT_PROVISIONED',
  'PROJECTION_UNAVAILABLE',
])

export const apiErrorSchema = z.object({
  code: errorCodeSchema,
  error: z.string().min(1),
  details: z
    .object({
      callsigns: z.array(z.string().min(1)).optional(),
      checkpointName: z.string().min(1).optional(),
      expectedVersion: z.number().int().min(0).optional(),
      currentVersion: z.number().int().min(0).optional(),
      status: missionStatusSchema.optional(),
      transition: missionTransitionSchema.optional(),
    })
    .optional(),
})

export type MissionStatus = z.infer<typeof missionStatusSchema>
export type MissionTransition = z.infer<typeof missionTransitionSchema>
export type CheckpointState = z.infer<typeof checkpointStateSchema>
export type AbortReason = z.infer<typeof abortReasonSchema>
export type NamedReference = z.infer<typeof namedReferenceSchema>
export type OperatingWindow = z.infer<typeof windowSchema>
export type MissionCheckpoint = z.infer<typeof missionCheckpointSchema>
export type MissionAssignment = z.infer<typeof missionAssignmentSchema>
export type MissionCoverage = z.infer<typeof missionCoverageSchema>
export type Mission = z.infer<typeof missionSchema>
export type MissionList = z.infer<typeof missionListSchema>
export type CreateMissionRequest = z.infer<typeof createMissionRequestSchema>
export type TransitionRequest = z.infer<typeof transitionRequestSchema>
export type ErrorCode = z.infer<typeof errorCodeSchema>
export type ApiError = z.infer<typeof apiErrorSchema>
