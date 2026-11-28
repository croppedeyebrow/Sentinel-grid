import { describe, expect, it } from 'vitest'
import example from '../../../../contracts/examples/operational-picture.json'
import {
  applyLiveMessage,
  changedUnitIds,
  emptyFeed,
  liveMessageSchema,
  type FeedState,
  type LiveMessage,
} from '../entities/live-feed/model'
import { operationalPictureSchema, type OperationalPictureDto } from '../entities/patrol-unit/model'

// The client's half of the live feed. These rules decide whether an operator
// is looking at a picture the platform would agree with, so they are tested
// against the published example rather than a hand-made shape.

const picture: OperationalPictureDto = operationalPictureSchema.parse(example)

const snapshot: LiveMessage = {
  kind: 'SNAPSHOT',
  revision: 1,
  sentAt: '2026-11-28T10:00:00Z',
  picture,
}

function started(): FeedState {
  return applyLiveMessage(emptyFeed, snapshot).state
}

function unitsMessage(revision: number, callsign: string, battery: number): LiveMessage {
  const unit = picture.units.find((candidate) => candidate.callsign === callsign)
  if (unit === undefined) throw new Error(`the example has no ${callsign}`)

  return {
    kind: 'UNITS',
    revision,
    sentAt: '2026-11-28T10:00:05Z',
    units: [{ ...unit, battery: { ...unit.battery, value: battery } }],
  }
}

describe('live message contract', () => {
  it('accepts the three message kinds', () => {
    expect(liveMessageSchema.parse(snapshot).kind).toBe('SNAPSHOT')
    expect(liveMessageSchema.parse(unitsMessage(2, 'SG-01', 71)).kind).toBe('UNITS')
    expect(
      liveMessageSchema.parse({
        kind: 'INGEST',
        revision: 2,
        sentAt: '2026-11-28T10:00:05Z',
        ingest: { state: 'NOT_RECEIVING', lastEventAt: null },
      }).kind,
    ).toBe('INGEST')
  })

  it('refuses a units message that carries no unit', () => {
    // Nothing is pushed for a change that changed nothing, so an empty list is
    // a server bug the client should not paper over.
    expect(() =>
      liveMessageSchema.parse({
        kind: 'UNITS',
        revision: 2,
        sentAt: '2026-11-28T10:00:05Z',
        units: [],
      }),
    ).toThrow()
  })

  it('refuses an unknown kind', () => {
    expect(() =>
      liveMessageSchema.parse({ kind: 'PATCH', revision: 2, sentAt: '2026-11-28T10:00:05Z' }),
    ).toThrow()
  })
})

describe('applyLiveMessage', () => {
  it('starts from a whole picture', () => {
    const outcome = applyLiveMessage(emptyFeed, snapshot)

    expect(outcome.effect).toBe('APPLIED')
    expect(outcome.state.revision).toBe(1)
    expect(outcome.state.picture?.units).toHaveLength(picture.units.length)
  })

  it('asks for a picture when a delta arrives before any snapshot', () => {
    const outcome = applyLiveMessage(emptyFeed, unitsMessage(2, 'SG-01', 71))

    expect(outcome.effect).toBe('RESYNC')
    expect(outcome.state.picture).toBeNull()
  })

  it('applies the next revision and leaves every other unit alone', () => {
    const outcome = applyLiveMessage(started(), unitsMessage(2, 'SG-01', 71))

    expect(outcome.effect).toBe('APPLIED')
    const changed = outcome.state.picture?.units.find((unit) => unit.callsign === 'SG-01')
    expect(changed?.battery.value).toBe(71)

    const others = outcome.state.picture?.units.filter((unit) => unit.callsign !== 'SG-01')
    const before = picture.units.filter((unit) => unit.callsign !== 'SG-01')
    expect(others).toEqual(before)
  })

  it('keeps the order the picture had', () => {
    const outcome = applyLiveMessage(started(), unitsMessage(2, 'SG-03', 44))

    expect(outcome.state.picture?.units.map((unit) => unit.callsign)).toEqual(
      picture.units.map((unit) => unit.callsign),
    )
  })

  it('applying the same delta twice leaves the same picture', () => {
    // Re-delivery is normal, and the screen must not treat it as news.
    const first = applyLiveMessage(started(), unitsMessage(2, 'SG-01', 71))
    const second = applyLiveMessage(first.state, unitsMessage(2, 'SG-01', 71))

    expect(second.effect).toBe('IGNORED')
    expect(second.state).toEqual(first.state)
  })

  it('asks for a whole picture when a revision is missing', () => {
    // The client knows it missed revision 2. Applying 3 would render a picture
    // built on a change it never received.
    const outcome = applyLiveMessage(started(), unitsMessage(3, 'SG-01', 71))

    expect(outcome.effect).toBe('RESYNC')
    expect(outcome.state.awaitingResync).toBe(true)
    expect(outcome.state.revision).toBe(1)
    expect(outcome.state.picture).toEqual(picture)
  })

  it('ends a resync with the snapshot that answers it', () => {
    const gapped = applyLiveMessage(started(), unitsMessage(3, 'SG-01', 71))
    const resynced = applyLiveMessage(gapped.state, {
      ...snapshot,
      revision: 4,
      sentAt: '2026-11-28T10:00:09Z',
    })

    expect(resynced.state.awaitingResync).toBe(false)
    expect(resynced.state.revision).toBe(4)
  })

  it('replaces only the intake on an ingest message', () => {
    const outcome = applyLiveMessage(started(), {
      kind: 'INGEST',
      revision: 2,
      sentAt: '2026-11-28T10:00:05Z',
      ingest: { state: 'NOT_RECEIVING', lastEventAt: null },
    })

    expect(outcome.effect).toBe('APPLIED')
    expect(outcome.state.picture?.ingest.state).toBe('NOT_RECEIVING')
    expect(outcome.state.picture?.units).toEqual(picture.units)
  })

  it('adds a unit the picture has never seen', () => {
    const newcomer = { ...picture.units[0], id: 'unit-new', callsign: 'SG-09' }
    const outcome = applyLiveMessage(started(), {
      kind: 'UNITS',
      revision: 2,
      sentAt: '2026-11-28T10:00:05Z',
      units: [newcomer],
    })

    expect(outcome.state.picture?.units).toHaveLength(picture.units.length + 1)
    expect(outcome.state.picture?.units.at(-1)?.callsign).toBe('SG-09')
  })
})

describe('changedUnitIds', () => {
  it('names the units whose reading differs', () => {
    const after = applyLiveMessage(started(), unitsMessage(2, 'SG-01', 71)).state.picture

    const changed = changedUnitIds(picture, after)
    expect(changed).toHaveLength(1)
    expect(changed[0]).toBe(picture.units.find((unit) => unit.callsign === 'SG-01')?.id)
  })

  it('names nothing when nothing moved', () => {
    expect(changedUnitIds(picture, picture)).toEqual([])
    expect(changedUnitIds(null, picture)).toEqual([])
  })
})
