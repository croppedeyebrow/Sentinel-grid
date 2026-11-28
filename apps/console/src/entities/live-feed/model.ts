import { z } from 'zod'
import {
  ingestSchema,
  operationalPictureSchema,
  patrolUnitSchema,
  type OperationalPictureDto,
} from '../patrol-unit/model'

/**
 * The client half of the live feed.
 *
 * This file holds the rules, not the socket: what a message is allowed to do
 * to the picture on screen, and what the client does when it can tell it
 * missed one. Keeping it pure is what makes "a gap forces a resync" testable
 * without a server, and it is the rule most likely to be got wrong in a
 * component.
 */

const isoDateTime = z.string().datetime({ offset: true })

export const liveMessageSchema = z.discriminatedUnion('kind', [
  z.object({
    kind: z.literal('SNAPSHOT'),
    revision: z.number().int().positive(),
    sentAt: isoDateTime,
    picture: operationalPictureSchema,
  }),
  z.object({
    kind: z.literal('UNITS'),
    revision: z.number().int().positive(),
    sentAt: isoDateTime,
    /** Only the units that changed, whole. Never empty. */
    units: z.array(patrolUnitSchema).min(1),
  }),
  z.object({
    kind: z.literal('INGEST'),
    revision: z.number().int().positive(),
    sentAt: isoDateTime,
    ingest: ingestSchema,
  }),
])

export type LiveMessage = z.infer<typeof liveMessageSchema>

/** How the screen is being fed, which the operator has to be able to see. */
export type Liveness = 'CONNECTING' | 'LIVE' | 'POLLING'

export interface FeedState {
  /** The last revision applied, null before the first snapshot. */
  revision: number | null
  /** The picture as the feed left it, null until a snapshot arrives. */
  picture: OperationalPictureDto | null
  /** When the last applied message was sent. */
  sentAt: string | null
  /** True while the client knows it is missing something and has asked again. */
  awaitingResync: boolean
}

export const emptyFeed: FeedState = {
  revision: null,
  picture: null,
  sentAt: null,
  awaitingResync: false,
}

/** What the caller must do about the message it just handed over. */
export type FeedEffect =
  /** Applied; nothing else to do. */
  | 'APPLIED'
  /** Already seen, so it changed nothing. */
  | 'IGNORED'
  /** A revision is missing: ask the server for a whole picture. */
  | 'RESYNC'

export interface FeedOutcome {
  state: FeedState
  effect: FeedEffect
}

/**
 * Applies one message to the feed state.
 *
 * Three rules decide everything:
 *
 * - a snapshot replaces the picture and resets what the client expects, which
 *   is how a resync ends;
 * - the next revision applies;
 * - anything else is either already seen, and ignored, or past a gap, and then
 *   the client asks for a whole picture rather than rendering one built on
 *   changes it never received. A screen that silently skips a change is worse
 *   than one that reloads.
 */
export function applyLiveMessage(state: FeedState, message: LiveMessage): FeedOutcome {
  if (message.kind === 'SNAPSHOT') {
    return {
      state: {
        revision: message.revision,
        picture: message.picture,
        sentAt: message.sentAt,
        awaitingResync: false,
      },
      effect: 'APPLIED',
    }
  }

  // A delta without a picture to change is not something to guess at.
  if (state.revision === null || state.picture === null) {
    return { state: { ...state, awaitingResync: true }, effect: 'RESYNC' }
  }

  if (message.revision <= state.revision) {
    // Re-delivery. Applying it again would be harmless, but saying so keeps
    // the caller from treating it as news.
    return { state, effect: 'IGNORED' }
  }

  if (message.revision > state.revision + 1) {
    return { state: { ...state, awaitingResync: true }, effect: 'RESYNC' }
  }

  const picture =
    message.kind === 'UNITS'
      ? { ...state.picture, units: replaceUnits(state.picture.units, message.units) }
      : { ...state.picture, ingest: message.ingest }

  return {
    state: {
      revision: message.revision,
      picture,
      sentAt: message.sentAt,
      awaitingResync: false,
    },
    effect: 'APPLIED',
  }
}

/**
 * Replaces each changed unit by id, keeping the order the picture had.
 *
 * A unit nobody mentioned is left exactly as it was: it keeps its last reading
 * and keeps ageing against the policy, because nothing new was measured. A
 * unit the picture has never seen is appended — a newly registered unit is a
 * change like any other.
 */
function replaceUnits(
  current: OperationalPictureDto['units'],
  changed: OperationalPictureDto['units'],
): OperationalPictureDto['units'] {
  const byId = new Map(changed.map((unit) => [unit.id, unit]))
  const merged = current.map((unit) => byId.get(unit.id) ?? unit)

  const known = new Set(current.map((unit) => unit.id))
  const added = changed.filter((unit) => !known.has(unit.id))
  return added.length === 0 ? merged : [...merged, ...added]
}

/** The units whose reading differs between two pictures, by id. */
export function changedUnitIds(
  before: OperationalPictureDto | null,
  after: OperationalPictureDto | null,
): readonly string[] {
  if (before === null || after === null) return []

  const previous = new Map(before.units.map((unit) => [unit.id, unit]))
  return after.units
    .filter((unit) => {
      const earlier = previous.get(unit.id)
      return earlier !== undefined && JSON.stringify(earlier) !== JSON.stringify(unit)
    })
    .map((unit) => unit.id)
}
