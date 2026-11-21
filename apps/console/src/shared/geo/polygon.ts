import type { PatrolArea, Position } from '../../entities/patrol-unit/model'

/**
 * Point-in-polygon test used while planning, so the operator sees a
 * checkpoint fall outside the patrol area as they place it.
 *
 * This is feedback, not authority: the server re-checks containment with
 * PostGIS, which is the only judgement that counts. Holes are not handled
 * because the API rejects polygons with holes for now.
 */
export function isInsideRing(point: Position, ring: readonly (readonly number[])[]): boolean {
  let inside = false

  for (let current = 0, previous = ring.length - 1; current < ring.length; previous = current++) {
    const [currentLon, currentLat] = ring[current]
    const [previousLon, previousLat] = ring[previous]

    const straddlesLatitude = currentLat > point.latitude !== previousLat > point.latitude
    if (!straddlesLatitude) continue

    const crossingLon =
      ((previousLon - currentLon) * (point.latitude - currentLat)) / (previousLat - currentLat) +
      currentLon
    if (point.longitude < crossingLon) inside = !inside
  }

  return inside
}

export function isInsideArea(point: Position, area: PatrolArea): boolean {
  const ring = area.boundary.coordinates[0]
  return ring === undefined ? false : isInsideRing(point, ring)
}
