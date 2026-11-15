import { describe, expect, it } from 'vitest'
import { operationalPictureSchema } from '../entities/patrol-unit/model'
import { readPublishedExample } from './publishedContract'

/**
 * The console's runtime schema is checked against the published example, so a
 * contract change cannot land on the API side alone: the Go contract test and
 * this test read the same two files in contracts/.
 */
describe('published Operational Picture contract', () => {
  const example: unknown = readPublishedExample()

  it('is accepted by the console schema', () => {
    expect(() => operationalPictureSchema.parse(example)).not.toThrow()
  })

  it('covers every unit status and every freshness value', () => {
    const picture = operationalPictureSchema.parse(example)

    expect(new Set(picture.units.map((unit) => unit.status))).toEqual(
      new Set(['NORMAL', 'WARNING', 'CRITICAL', 'UNKNOWN']),
    )
    const freshness = picture.units.flatMap((unit) => [
      unit.position.freshness,
      unit.battery.freshness,
      unit.link.freshness,
    ])
    expect(new Set(freshness)).toEqual(new Set(['FRESH', 'STALE', 'MISSING']))
  })

  it('keeps a last known value on an observation it no longer trusts', () => {
    const picture = operationalPictureSchema.parse(example)
    const disconnected = picture.units.find((unit) => unit.position.freshness === 'MISSING')

    expect(disconnected?.battery.value).not.toBeNull()
    expect(disconnected?.battery.freshness).toBe('MISSING')
  })

  it('rejects a payload that collapses status and freshness into one field', () => {
    const collapsed = {
      generatedAt: '2026-11-12T09:00:00Z',
      site: (example as { site: unknown }).site,
      patrolAreas: [],
      patrolRoutes: [],
      units: [{ id: 'u1', callsign: 'SG-01', displayName: 'One', status: 'STALE' }],
    }

    expect(() => operationalPictureSchema.parse(collapsed)).toThrow()
  })
})
