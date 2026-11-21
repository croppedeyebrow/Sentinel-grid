import { useEffect, useRef, useState } from 'react'
import {
  Map,
  Marker,
  NavigationControl,
  Popup,
  type GeoJSONSource,
  type MapMouseEvent,
  type StyleSpecification,
} from 'maplibre-gl'
import type { CheckpointState } from '../../entities/mission/model'
import { checkpointStateLabel } from '../../entities/mission/uiModel'
import type { PatrolArea, PatrolRoute, Position } from '../../entities/patrol-unit/model'

/**
 * Mission planning and progress map: the patrol area and planned route as
 * context, checkpoints as ordered markers coloured by their judgement. While
 * planning, clicking the map places the next checkpoint.
 */

const blankStyle: StyleSpecification = {
  version: 8,
  sources: {},
  layers: [{ id: 'background', type: 'background', paint: { 'background-color': '#0a1728' } }],
}

const AREA_SOURCE = 'mission-area'
const ROUTE_SOURCE = 'mission-route'

const stateColor: Record<CheckpointState, string> = {
  PLANNED: '#7ea2ff',
  VISITED: '#3ee6a8',
  MISSED: '#ff6577',
  UNKNOWN: '#8ea4c4',
}

export interface MissionMapCheckpoint {
  sequence: number
  name: string
  position: Position
  state: CheckpointState
}

interface MissionMapProps {
  area: PatrolArea
  route: PatrolRoute | null
  checkpoints: readonly MissionMapCheckpoint[]
  planning: boolean
  onPlaceCheckpoint?: (position: Position) => void
}

function checkpointElement(checkpoint: MissionMapCheckpoint) {
  const element = document.createElement('button')
  element.type = 'button'
  element.className = `checkpoint-marker checkpoint-marker--${checkpoint.state.toLowerCase()}`
  element.style.setProperty('--marker-color', stateColor[checkpoint.state])
  element.textContent = String(checkpoint.sequence)
  element.setAttribute(
    'aria-label',
    `${checkpoint.name} ${checkpointStateLabel[checkpoint.state]}`,
  )
  return element
}

export function MissionMap({
  area,
  route,
  checkpoints,
  planning,
  onPlaceCheckpoint,
}: MissionMapProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const mapRef = useRef<Map | null>(null)
  const markersRef = useRef<Marker[]>([])
  const placeRef = useRef(onPlaceCheckpoint)
  const planningRef = useRef(planning)
  const [ready, setReady] = useState(false)

  placeRef.current = onPlaceCheckpoint
  planningRef.current = planning

  useEffect(() => {
    if (!containerRef.current || mapRef.current) return

    const map = new Map({
      container: containerRef.current,
      style: blankStyle,
      center: [126.978, 37.5665],
      zoom: 12.5,
      attributionControl: false,
    })
    mapRef.current = map
    map.addControl(new NavigationControl(), 'top-right')

    const onLoad = () => setReady(true)
    // The handler reads the latest props through refs so placing a checkpoint
    // never requires tearing the map down and rebuilding it.
    const onClick = (event: MapMouseEvent) => {
      if (!planningRef.current) return
      placeRef.current?.({ latitude: event.lngLat.lat, longitude: event.lngLat.lng })
    }
    map.on('load', onLoad)
    map.on('click', onClick)

    return () => {
      map.off('load', onLoad)
      map.off('click', onClick)
      setReady(false)
      map.remove()
      mapRef.current = null
    }
  }, [])

  useEffect(() => {
    const map = mapRef.current
    if (!map || !ready) return

    const areaData = {
      type: 'FeatureCollection' as const,
      features: [
        { type: 'Feature' as const, properties: { name: area.name }, geometry: area.boundary },
      ],
    }
    const routeData = {
      type: 'FeatureCollection' as const,
      features:
        route === null
          ? []
          : [{ type: 'Feature' as const, properties: { name: route.name }, geometry: route.path }],
    }

    const areaSource = map.getSource(AREA_SOURCE) as GeoJSONSource | undefined
    if (areaSource) {
      areaSource.setData(areaData)
    } else {
      map.addSource(AREA_SOURCE, { type: 'geojson', data: areaData })
      map.addLayer({
        id: 'mission-area-fill',
        type: 'fill',
        source: AREA_SOURCE,
        paint: { 'fill-color': '#2f6df6', 'fill-opacity': 0.08 },
      })
      map.addLayer({
        id: 'mission-area-outline',
        type: 'line',
        source: AREA_SOURCE,
        paint: { 'line-color': '#4d82ff', 'line-width': 1.5, 'line-opacity': 0.8 },
      })
    }

    const routeSource = map.getSource(ROUTE_SOURCE) as GeoJSONSource | undefined
    if (routeSource) {
      routeSource.setData(routeData)
    } else {
      map.addSource(ROUTE_SOURCE, { type: 'geojson', data: routeData })
      map.addLayer({
        id: 'mission-route-line',
        type: 'line',
        source: ROUTE_SOURCE,
        layout: { 'line-cap': 'round', 'line-join': 'round' },
        paint: { 'line-color': '#7ea2ff', 'line-width': 2, 'line-dasharray': [2, 2] },
      })
    }
  }, [ready, area, route])

  useEffect(() => {
    const map = mapRef.current
    if (!map || !ready) return

    for (const marker of markersRef.current) marker.remove()
    markersRef.current = []

    for (const checkpoint of checkpoints) {
      const marker = new Marker({ element: checkpointElement(checkpoint) })
        .setLngLat([checkpoint.position.longitude, checkpoint.position.latitude])
        .setPopup(
          new Popup({ offset: 16, closeButton: false }).setHTML(
            `<strong>${checkpoint.name}</strong><br>${checkpointStateLabel[checkpoint.state]}`,
          ),
        )
        .addTo(map)
      markersRef.current.push(marker)
    }
  }, [ready, checkpoints])

  return (
    <div className={`mission-map${planning ? ' mission-map--planning' : ''}`}>
      <div ref={containerRef} className="mission-map__canvas" aria-label="임무 계획 지도" />
      {planning ? (
        <p className="mission-map__hint">지도를 클릭해 체크포인트를 순서대로 추가하세요.</p>
      ) : null}
    </div>
  )
}
