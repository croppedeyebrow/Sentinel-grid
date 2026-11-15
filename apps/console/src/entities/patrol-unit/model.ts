import { z } from 'zod'

/**
 * Runtime contract for the Operational Picture response.
 *
 * Unit condition (`status`) and observation quality (`freshness`) are separate
 * fields on purpose: a unit can be healthy while its telemetry is stale, and
 * the console has to be able to say both at once.
 */

export const unitStatusSchema = z.enum(['NORMAL', 'WARNING', 'CRITICAL', 'UNKNOWN'])
export const freshnessSchema = z.enum(['FRESH', 'STALE', 'MISSING'])

const isoDateTime = z.string().datetime({ offset: true })

export const positionSchema = z.object({
  latitude: z.number().min(-90).max(90),
  longitude: z.number().min(-180).max(180),
})

const coordinateSchema = z.tuple([z.number(), z.number()])

export const observationPolicySchema = z.object({
  positionStaleAfterSeconds: z.number().int().positive(),
  batteryStaleAfterSeconds: z.number().int().positive(),
  linkStaleAfterSeconds: z.number().int().positive(),
  batteryWarningPercent: z.number().int().min(0).max(100),
  batteryCriticalPercent: z.number().int().min(0).max(100),
  linkWarningPercent: z.number().int().min(0).max(100),
  linkCriticalPercent: z.number().int().min(0).max(100),
})

export const siteProfileSchema = z.object({
  id: z.string().min(1),
  name: z.string().min(1),
  center: positionSchema,
  policy: observationPolicySchema,
})

export const patrolAreaSchema = z.object({
  id: z.string().min(1),
  name: z.string().min(1),
  boundary: z.object({
    type: z.literal('Polygon'),
    coordinates: z.array(z.array(coordinateSchema).min(4)).min(1),
  }),
})

export const patrolRouteSchema = z.object({
  id: z.string().min(1),
  patrolAreaId: z.string().min(1),
  name: z.string().min(1),
  path: z.object({
    type: z.literal('LineString'),
    coordinates: z.array(coordinateSchema).min(2),
  }),
})

const observed = <T extends z.ZodTypeAny>(value: T) =>
  z.object({
    value: value.nullable(),
    observedAt: isoDateTime.nullable(),
    receivedAt: isoDateTime.nullable(),
    freshness: freshnessSchema,
  })

export const observedPositionSchema = observed(positionSchema)
export const observedPercentSchema = observed(z.number().int().min(0).max(100))

export const patrolUnitSchema = z.object({
  id: z.string().min(1),
  callsign: z.string().min(1),
  displayName: z.string().min(1),
  status: unitStatusSchema,
  position: observedPositionSchema,
  battery: observedPercentSchema,
  link: observedPercentSchema,
})

export const operationalPictureSchema = z.object({
  generatedAt: isoDateTime,
  site: siteProfileSchema,
  patrolAreas: z.array(patrolAreaSchema),
  patrolRoutes: z.array(patrolRouteSchema),
  units: z.array(patrolUnitSchema),
})

export type UnitStatus = z.infer<typeof unitStatusSchema>
export type Freshness = z.infer<typeof freshnessSchema>
export type Position = z.infer<typeof positionSchema>
export type ObservationPolicy = z.infer<typeof observationPolicySchema>
export type SiteProfile = z.infer<typeof siteProfileSchema>
export type PatrolArea = z.infer<typeof patrolAreaSchema>
export type PatrolRoute = z.infer<typeof patrolRouteSchema>
export type ObservedPosition = z.infer<typeof observedPositionSchema>
export type ObservedPercent = z.infer<typeof observedPercentSchema>
export type PatrolUnitDto = z.infer<typeof patrolUnitSchema>
export type OperationalPictureDto = z.infer<typeof operationalPictureSchema>
