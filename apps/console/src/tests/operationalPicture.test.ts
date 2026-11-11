import { describe, expect, it } from 'vitest'
import { operationalPictureSchema, statusLabel } from '../entities/patrol-unit/model'

describe('operationalPictureSchema', () => {
  it('accepts NORMAL, STALE, and MISSING units', () => {
    const payload = {
      generatedAt: '2026-11-11T00:00:00Z',
      units: ['NORMAL', 'STALE', 'MISSING'].map((status, index) => ({
        id: `sg-${index}`, callsign: `SG-0${index}`, displayName: `Sentinel ${index}`, status,
        latitude: status === 'MISSING' ? null : 37.5665,
        longitude: status === 'MISSING' ? null : 126.978,
        batteryLevel: status === 'MISSING' ? null : 82,
        linkQuality: 94,
        observedAt: status === 'MISSING' ? null : '2026-11-11T00:00:00Z',
        receivedAt: '2026-11-11T00:00:00Z',
      })),
    }
    expect(operationalPictureSchema.parse(payload).units).toHaveLength(3)
  })

  it('provides a distinct label for every status', () => {
    expect(new Set(Object.values(statusLabel)).size).toBe(3)
  })
})

