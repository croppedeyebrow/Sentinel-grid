import type {
  Freshness,
  Ingest,
  IngestState,
  ObservationPolicy,
  ObservedPercent,
  ObservedPosition,
  PatrolUnitDto,
  Position,
  StreamState,
  UnitStatus,
  UnitStream,
} from './model'

/**
 * Adapter from the API response to the model the components render. Keeping it
 * here means no component has to re-derive a verdict or re-apply a threshold,
 * and the two axes stay labelled separately all the way to the screen.
 */

export const statusLabel: Record<UnitStatus, string> = {
  NORMAL: '정상',
  WARNING: '주의',
  CRITICAL: '위험',
  UNKNOWN: '판단 불가',
}

export const freshnessLabel: Record<Freshness, string> = {
  FRESH: '최신',
  STALE: '지연',
  MISSING: '누락',
}

export type StatusTone = 'normal' | 'warning' | 'critical' | 'unknown'

export const statusTone: Record<UnitStatus, StatusTone> = {
  NORMAL: 'normal',
  WARNING: 'warning',
  CRITICAL: 'critical',
  UNKNOWN: 'unknown',
}

export interface UiObservation {
  freshness: Freshness
  freshnessLabel: string
  /** A value is trusted only while it is fresh; stale values stay visible but marked. */
  trusted: boolean
  valueLabel: string
  observedAgoLabel: string | null
  receivedAgoLabel: string | null
}

export interface UiPosition extends UiObservation {
  coordinates: Position | null
  /** Whether the unit can be drawn on the map at all. */
  mappable: boolean
}

export interface UiPatrolUnit {
  id: string
  callsign: string
  displayName: string
  status: UnitStatus
  statusLabel: string
  tone: StatusTone
  position: UiPosition
  battery: UiObservation
  link: UiObservation
  /** Fields whose observation is not fresh, named for the operator. */
  evidenceGaps: string[]
  statusExplanation: string
  /** Telemetry health, which answers whether anything is arriving at all. */
  stream: UiStream
}

const SECOND = 1000
const MINUTE = 60 * SECOND
const HOUR = 60 * MINUTE

/** Formats an age the way an operator reads it, not as a raw timestamp. */
export function formatAge(timestamp: string | null, now: Date): string | null {
  if (timestamp === null) return null

  const elapsed = now.getTime() - new Date(timestamp).getTime()
  if (Number.isNaN(elapsed)) return null
  if (elapsed < 0) return '방금'
  if (elapsed < MINUTE) return `${Math.floor(elapsed / SECOND)}초 전`
  if (elapsed < HOUR) return `${Math.floor(elapsed / MINUTE)}분 전`

  const hours = Math.floor(elapsed / HOUR)
  const minutes = Math.floor((elapsed % HOUR) / MINUTE)
  return minutes === 0 ? `${hours}시간 전` : `${hours}시간 ${minutes}분 전`
}

function observation(
  observed: ObservedPercent,
  now: Date,
  suffix: string,
): UiObservation {
  return {
    freshness: observed.freshness,
    freshnessLabel: freshnessLabel[observed.freshness],
    trusted: observed.freshness === 'FRESH',
    valueLabel: observed.value === null ? '관측 없음' : `${observed.value}${suffix}`,
    observedAgoLabel: formatAge(observed.observedAt, now),
    receivedAgoLabel: formatAge(observed.receivedAt, now),
  }
}

function positionObservation(observed: ObservedPosition, now: Date): UiPosition {
  const coordinates = observed.value
  return {
    freshness: observed.freshness,
    freshnessLabel: freshnessLabel[observed.freshness],
    trusted: observed.freshness === 'FRESH',
    valueLabel:
      coordinates === null
        ? '위치 없음'
        : `${coordinates.latitude.toFixed(4)}, ${coordinates.longitude.toFixed(4)}`,
    observedAgoLabel: formatAge(observed.observedAt, now),
    receivedAgoLabel: formatAge(observed.receivedAt, now),
    coordinates,
    mappable: coordinates !== null,
  }
}

const fieldNames = { position: '위치', battery: '배터리', link: '링크' } as const

function evidenceGaps(unit: PatrolUnitDto): string[] {
  return (['position', 'battery', 'link'] as const)
    .filter((field) => unit[field].freshness !== 'FRESH')
    .map((field) => `${fieldNames[field]} 관측 ${freshnessLabel[unit[field].freshness]}`)
}

/**
 * Explains the verdict the API already made. The console never recomputes a
 * status; it only says which observation or threshold produced it.
 */
function explainStatus(unit: PatrolUnitDto, policy: ObservationPolicy, gaps: string[]): string {
  if (unit.status === 'UNKNOWN') {
    return gaps.length > 0
      ? `근거 부족: ${gaps.join(', ')}`
      : '근거가 부족해 상태를 단정하지 않습니다.'
  }

  const reasons: string[] = []
  const battery = unit.battery.value
  const link = unit.link.value
  if (unit.status === 'CRITICAL') {
    if (battery !== null && battery <= policy.batteryCriticalPercent) {
      reasons.push(`배터리 ${battery}% (위험 기준 ${policy.batteryCriticalPercent}% 이하)`)
    }
    if (link !== null && link <= policy.linkCriticalPercent) {
      reasons.push(`링크 ${link}% (위험 기준 ${policy.linkCriticalPercent}% 이하)`)
    }
  }
  if (unit.status === 'WARNING') {
    if (battery !== null && battery <= policy.batteryWarningPercent) {
      reasons.push(`배터리 ${battery}% (주의 기준 ${policy.batteryWarningPercent}% 이하)`)
    }
    if (link !== null && link <= policy.linkWarningPercent) {
      reasons.push(`링크 ${link}% (주의 기준 ${policy.linkWarningPercent}% 이하)`)
    }
  }
  if (reasons.length > 0) return reasons.join(', ')
  return '모든 관측이 최신이고 기준 이내입니다.'
}

export const streamStateLabel: Record<StreamState, string> = {
  STREAMING: '수신 중',
  SILENT: '수신 끊김',
  NEVER_SEEN: '수신 기록 없음',
}

export interface UiStream {
  state: StreamState
  label: string
  /** True while frames are arriving, which is not the same as values being current. */
  arriving: boolean
  lastReceivedAgoLabel: string | null
  sessionAgoLabel: string | null
  reconnected: boolean
  missingFrames: number
  rejectedFrames: number
  /** Short sentence naming what is wrong with the stream, empty when nothing is. */
  warning: string
}

/**
 * Describes the telemetry health of a unit. Kept apart from the condition axis
 * on purpose: a unit that stopped transmitting is undecided, not failing.
 */
export function toUiStream(stream: UnitStream, now: Date): UiStream {
  const problems: string[] = []
  if (stream.state === 'SILENT') problems.push('프레임이 도착하지 않습니다')
  if (stream.state === 'NEVER_SEEN') problems.push('수신 기록이 없습니다')
  if (stream.missingFrames > 0) problems.push(`누락 ${stream.missingFrames}프레임`)
  if (stream.rejectedFrames > 0) problems.push(`거부 ${stream.rejectedFrames}프레임`)

  return {
    state: stream.state,
    label: streamStateLabel[stream.state],
    arriving: stream.state === 'STREAMING',
    lastReceivedAgoLabel: formatAge(stream.lastReceivedAt, now),
    sessionAgoLabel: formatAge(stream.sessionStartedAt, now),
    reconnected: stream.reconnected,
    missingFrames: stream.missingFrames,
    rejectedFrames: stream.rejectedFrames,
    warning: problems.join(' · '),
  }
}

export function toUiPatrolUnit(
  unit: PatrolUnitDto,
  policy: ObservationPolicy,
  now: Date,
): UiPatrolUnit {
  const gaps = evidenceGaps(unit)
  return {
    id: unit.id,
    callsign: unit.callsign,
    displayName: unit.displayName,
    status: unit.status,
    statusLabel: statusLabel[unit.status],
    tone: statusTone[unit.status],
    position: positionObservation(unit.position, now),
    battery: observation(unit.battery, now, '%'),
    link: observation(unit.link, now, '%'),
    evidenceGaps: gaps,
    statusExplanation: explainStatus(unit, policy, gaps),
    stream: toUiStream(unit.stream, now),
  }
}

export function summarizeStatuses(units: readonly UiPatrolUnit[]): Record<UnitStatus, number> {
  const summary: Record<UnitStatus, number> = { NORMAL: 0, WARNING: 0, CRITICAL: 0, UNKNOWN: 0 }
  for (const unit of units) summary[unit.status] += 1
  return summary
}

/** Counts units whose position cannot be trusted as current. */
export function countUntrustedPositions(units: readonly UiPatrolUnit[]): number {
  return units.filter((unit) => !unit.position.trusted).length
}

/** Units whose telemetry is not arriving at all. */
export function countSilentStreams(units: readonly UiPatrolUnit[]): number {
  return units.filter((unit) => !unit.stream.arriving).length
}
export const ingestStateLabel: Record<IngestState, string> = {
  RECEIVING: '수신 경로 정상',
  NOT_RECEIVING: '플랫폼이 수신하지 못하고 있습니다',
  UNKNOWN: '수신 경로 상태를 알 수 없습니다',
}

export interface UiIngest {
  state: IngestState
  label: string
  /** True only while the platform is taking telemetry in. */
  receiving: boolean
  /**
   * True when a unit's silence cannot be held against the unit, because the
   * platform itself is not receiving or cannot say.
   */
  silenceIsOurs: boolean
  lastEventAgoLabel: string | null
  /** What the operator has to understand, empty when nothing is wrong. */
  warning: string
}

/**
 * Describes the platform's own intake. This is read before any unit's silence
 * is interpreted: when the intake is broken every unit goes quiet, and a
 * screen that only showed per-unit silence would report a lost fleet instead
 * of a lost pipe.
 */
export function toUiIngest(ingest: Ingest, now: Date): UiIngest {
  const receiving = ingest.state === 'RECEIVING'
  const warning = receiving
    ? ''
    : ingest.state === 'NOT_RECEIVING'
      ? '수신 경로가 끊겼습니다. 순찰체의 침묵은 순찰체 문제가 아닐 수 있습니다.'
      : '수신 경로 상태를 확인할 수 없습니다. 순찰체의 침묵을 단정하지 마세요.'

  return {
    state: ingest.state,
    label: ingestStateLabel[ingest.state],
    receiving,
    silenceIsOurs: !receiving,
    lastEventAgoLabel: formatAge(ingest.lastEventAt, now),
    warning,
  }
}

export function describePolicy(policy: ObservationPolicy): string {
  return `위치 ${policy.positionStaleAfterSeconds}초 · 배터리 ${policy.batteryStaleAfterSeconds}초 · 링크 ${policy.linkStaleAfterSeconds}초 이후 지연`
}
