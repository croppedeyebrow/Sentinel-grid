import { useEffect, useRef } from 'react'
import { Map, Marker, NavigationControl, Popup, type StyleSpecification } from 'maplibre-gl'
import type { PatrolUnit } from '../../entities/patrol-unit/model'

const blankStyle: StyleSpecification = {
  version: 8,
  sources: {},
  layers: [{ id: 'background', type: 'background', paint: { 'background-color': '#0a1728' } }],
}

const markerColor = { NORMAL: '#3ee6a8', STALE: '#ffbf4b', MISSING: '#ff6577' } as const

export function PatrolMap({ units }: { units: PatrolUnit[] }) {
  const containerRef = useRef<HTMLDivElement>(null)
  const mapRef = useRef<Map | null>(null)
  const markersRef = useRef<Marker[]>([])

  useEffect(() => {
    if (!containerRef.current || mapRef.current) return
    mapRef.current = new Map({
      container: containerRef.current,
      style: blankStyle,
      center: [126.978, 37.5665],
      zoom: 12,
      attributionControl: false,
    })
    mapRef.current.addControl(new NavigationControl(), 'top-right')
    return () => {
      mapRef.current?.remove()
      mapRef.current = null
    }
  }, [])

  useEffect(() => {
    markersRef.current.forEach((marker) => marker.remove())
    markersRef.current = []
    const map = mapRef.current
    if (!map) return

    units.forEach((unit) => {
      if (unit.latitude === null || unit.longitude === null) return
      const element = document.createElement('button')
      element.className = `map-marker map-marker--${unit.status.toLowerCase()}`
      element.style.setProperty('--marker-color', markerColor[unit.status])
      element.setAttribute('aria-label', `${unit.callsign} ${unit.status}`)
      const popup = new Popup({ offset: 18 }).setHTML(
        `<strong>${unit.callsign}</strong><br>${unit.displayName}`,
      )
      const marker = new Marker({ element })
        .setLngLat([unit.longitude, unit.latitude])
        .setPopup(popup)
        .addTo(map)
      markersRef.current.push(marker)
    })
  }, [units])

  return <div ref={containerRef} className="patrol-map" aria-label="순찰체 위치 지도" />
}

