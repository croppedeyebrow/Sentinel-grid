import { useEffect, useRef, useState } from 'react'
import {
  Map,
  Marker,
  NavigationControl,
  Popup,
  ScaleControl,
  type GeoJSONSource,
  type StyleSpecification,
} from 'maplibre-gl'
import type { PatrolArea, PatrolRoute, SiteProfile } from '../../entities/patrol-unit/model'
import type { StatusTone, UiPatrolUnit } from '../../entities/patrol-unit/uiModel'

/**
 * The map draws the site context (patrol area, planned route) under the units
 * so a position is read against the area it belongs to. A position that is not
 * fresh is drawn differently instead of being moved or hidden: the operator
 * sees the last known location, marked as not current.
 */

const blankStyle: StyleSpecification = {
  version: 8,
  sources: {},
  layers: [{ id: 'background', type: 'background', paint: { 'background-color': '#0a1728' } }],
}

const AREA_SOURCE = 'patrol-areas'
const ROUTE_SOURCE = 'patrol-routes'

const toneColor: Record<StatusTone, string> = {
  normal: '#3ee6a8',
  warning: '#ffbf4b',
  critical: '#ff6577',
  unknown: '#8ea4c4',
}

interface PatrolMapProps {
  site: SiteProfile
  patrolAreas: readonly PatrolArea[]
  patrolRoutes: readonly PatrolRoute[]
  units: readonly UiPatrolUnit[]
}

function areaCollection(areas: readonly PatrolArea[]) {
  return {
    type: 'FeatureCollection' as const,
    features: areas.map((area) => ({
      type: 'Feature' as const,
      id: area.id,
      properties: { name: area.name },
      geometry: area.boundary,
    })),
  }
}

function routeCollection(routes: readonly PatrolRoute[]) {
  return {
    type: 'FeatureCollection' as const,
    features: routes.map((route) => ({
      type: 'Feature' as const,
      id: route.id,
      properties: { name: route.name },
      geometry: route.path,
    })),
  }
}

function markerElement(unit: UiPatrolUnit) {
  const element = document.createElement('button')
  element.type = 'button'
  element.className = `map-marker map-marker--${unit.tone}${
    unit.position.trusted ? '' : ' map-marker--untrusted'
  }`
  element.style.setProperty('--marker-color', toneColor[unit.tone])
  element.setAttribute(
    'aria-label',
    `${unit.callsign} ${unit.statusLabel}, 위치 ${unit.position.freshnessLabel}`,
  )
  element.textContent = unit.callsign.replace(/^SG-/, '')
  return element
}

function popupHtml(unit: UiPatrolUnit) {
  const age = unit.position.observedAgoLabel ?? unit.position.receivedAgoLabel ?? '관측 시각 없음'
  return `
    <strong>${unit.callsign}</strong> ${unit.displayName}
    <dl class="map-popup">
      <dt>상태</dt><dd>${unit.statusLabel}</dd>
      <dt>위치 관측</dt><dd>${unit.position.freshnessLabel} · ${age}</dd>
      <dt>배터리</dt><dd>${unit.battery.valueLabel} (${unit.battery.freshnessLabel})</dd>
      <dt>링크</dt><dd>${unit.link.valueLabel} (${unit.link.freshnessLabel})</dd>
    </dl>
    <p class="map-popup__reason">${unit.statusExplanation}</p>
  `
}

export function PatrolMap({ site, patrolAreas, patrolRoutes, units }: PatrolMapProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const mapRef = useRef<Map | null>(null)
  const markersRef = useRef<Marker[]>([])
  const [ready, setReady] = useState(false)

  useEffect(() => {
    if (!containerRef.current || mapRef.current) return

    const map = new Map({
      container: containerRef.current,
      style: blankStyle,
      center: [site.center.longitude, site.center.latitude],
      zoom: 12.5,
      attributionControl: false,
    })
    mapRef.current = map
    map.addControl(new NavigationControl(), 'top-right')
    map.addControl(new ScaleControl({ unit: 'metric' }), 'bottom-right')

    const onLoad = () => setReady(true)
    map.on('load', onLoad)

    return () => {
      map.off('load', onLoad)
      setReady(false)
      map.remove()
      mapRef.current = null
    }
  }, [site.center.latitude, site.center.longitude])

  // Site context: patrol area boundaries and the planned route.
  useEffect(() => {
    const map = mapRef.current
    if (!map || !ready) return

    const areas = areaCollection(patrolAreas)
    const routes = routeCollection(patrolRoutes)

    const areaSource = map.getSource(AREA_SOURCE) as GeoJSONSource | undefined
    if (areaSource) {
      areaSource.setData(areas)
    } else {
      map.addSource(AREA_SOURCE, { type: 'geojson', data: areas })
      map.addLayer({
        id: 'patrol-area-fill',
        type: 'fill',
        source: AREA_SOURCE,
        paint: { 'fill-color': '#2f6df6', 'fill-opacity': 0.08 },
      })
      map.addLayer({
        id: 'patrol-area-outline',
        type: 'line',
        source: AREA_SOURCE,
        paint: { 'line-color': '#4d82ff', 'line-width': 1.5, 'line-opacity': 0.8 },
      })
    }

    const routeSource = map.getSource(ROUTE_SOURCE) as GeoJSONSource | undefined
    if (routeSource) {
      routeSource.setData(routes)
    } else {
      map.addSource(ROUTE_SOURCE, { type: 'geojson', data: routes })
      map.addLayer({
        id: 'patrol-route-line',
        type: 'line',
        source: ROUTE_SOURCE,
        layout: { 'line-cap': 'round', 'line-join': 'round' },
        paint: {
          'line-color': '#7ea2ff',
          'line-width': 2,
          'line-dasharray': [2, 2],
          'line-opacity': 0.9,
        },
      })
    }
  }, [ready, patrolAreas, patrolRoutes])

  // Unit markers are rebuilt from the current response so a removed unit never
  // lingers on the map.
  useEffect(() => {
    const map = mapRef.current
    if (!map || !ready) return

    for (const marker of markersRef.current) marker.remove()
    markersRef.current = []

    for (const unit of units) {
      const coordinates = unit.position.coordinates
      if (!coordinates) continue

      const marker = new Marker({ element: markerElement(unit) })
        .setLngLat([coordinates.longitude, coordinates.latitude])
        .setPopup(new Popup({ offset: 18, closeButton: false }).setHTML(popupHtml(unit)))
        .addTo(map)
      markersRef.current.push(marker)
    }
  }, [ready, units])

  return (
    <div className="patrol-map">
      <div ref={containerRef} className="patrol-map__canvas" aria-label="순찰체 위치 지도" />
      <ul className="patrol-map__legend">
        <li>
          <span className="legend-swatch legend-swatch--area" /> 순찰구역 {patrolAreas.length}
        </li>
        <li>
          <span className="legend-swatch legend-swatch--route" /> 경로 {patrolRoutes.length}
        </li>
        <li>
          <span className="legend-swatch legend-swatch--untrusted" /> 위치 미신뢰
        </li>
      </ul>
    </div>
  )
}
