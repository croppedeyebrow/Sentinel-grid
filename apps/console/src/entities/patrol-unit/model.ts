import { z } from 'zod'

export const patrolUnitStatusSchema = z.enum(['NORMAL', 'STALE', 'MISSING'])

export const patrolUnitSchema = z.object({
  id: z.string().min(1),
  callsign: z.string().min(1),
  displayName: z.string().min(1),
  status: patrolUnitStatusSchema,
  latitude: z.number().nullable(),
  longitude: z.number().nullable(),
  batteryLevel: z.number().int().min(0).max(100).nullable(),
  linkQuality: z.number().int().min(0).max(100).nullable(),
  observedAt: z.string().datetime().nullable(),
  receivedAt: z.string().datetime().nullable(),
})

export const operationalPictureSchema = z.object({
  generatedAt: z.string().datetime(),
  units: z.array(patrolUnitSchema),
})

export type PatrolUnit = z.infer<typeof patrolUnitSchema>
export type PatrolUnitStatus = z.infer<typeof patrolUnitStatusSchema>
export const statusLabel: Record<PatrolUnitStatus, string> = {
  NORMAL: '정상',
  STALE: '위치 지연',
  MISSING: '정보 누락',
}

