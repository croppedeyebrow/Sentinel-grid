import type { PatrolArea } from '../patrol-unit/model'
import { isInsideArea } from '../../shared/geo/polygon'
import type {
  AbortReason,
  CheckpointState,
  Mission,
  MissionCoverage,
  MissionStatus,
} from './model'

export const missionStatusLabel: Record<MissionStatus, string> = {
  DRAFT: '초안',
  READY: '승인됨',
  ACTIVE: '진행 중',
  COMPLETED: '완료',
  ABORTED: '중단',
}

export const checkpointStateLabel: Record<CheckpointState, string> = {
  PLANNED: '미실행',
  VISITED: '통과',
  MISSED: '미순찰',
  UNKNOWN: '판단 불가',
}

export const abortReasonLabel: Record<AbortReason, string> = {
  OPERATOR: '운영자 지시',
  BATTERY: '배터리 부족',
  LINK: '통신 단절',
  SAFETY: '안전 위험',
  POLICY: '정책 제한',
}

export type MissionTransition = 'approve' | 'start' | 'complete' | 'abort'

export const transitionLabel: Record<MissionTransition, string> = {
  approve: '승인',
  start: '시작',
  complete: '완료',
  abort: '중단',
}

/**
 * Transitions the operator may attempt from a given state. The server remains
 * the authority; this only decides which controls are offered.
 *
 * An approved mission that has not started can still be called off, so
 * `READY → ABORTED` is offered alongside the transitions listed in the
 * scenario's table.
 */
export function allowedTransitions(status: MissionStatus): MissionTransition[] {
  switch (status) {
    case 'DRAFT':
      return ['approve', 'abort']
    case 'READY':
      return ['start', 'abort']
    case 'ACTIVE':
      return ['complete', 'abort']
    case 'COMPLETED':
    case 'ABORTED':
      return []
  }
}

export interface CoverageReading {
  visitedPercent: number
  missedPercent: number
  unknownPercent: number
  /** Checkpoints with a verdict, which is what a percentage may be read against. */
  judged: number
  total: number
  label: string
}

/**
 * Coverage is reported against the whole plan, and undecided checkpoints are
 * never folded into either side. A mission with half its checkpoints undecided
 * says so instead of claiming the rest as coverage.
 */
export function readCoverage(coverage: MissionCoverage): CoverageReading {
  const total = coverage.total
  const percent = (count: number) => (total === 0 ? 0 : Math.round((count / total) * 100))

  return {
    visitedPercent: percent(coverage.visited),
    missedPercent: percent(coverage.missed),
    unknownPercent: percent(coverage.unknown),
    judged: coverage.visited + coverage.missed,
    total,
    label:
      total === 0
        ? '체크포인트 없음'
        : `통과 ${coverage.visited} · 미순찰 ${coverage.missed} · 판단 불가 ${coverage.unknown} / ${total}`,
  }
}

export interface ApprovalReadiness {
  ready: boolean
  problems: string[]
}

/**
 * Preconditions the scenario puts on approval, evaluated locally so the
 * planner is told what is missing before sending the request.
 */
export function checkApprovalReadiness(mission: Mission, area: PatrolArea): ApprovalReadiness {
  const problems: string[] = []

  if (mission.checkpoints.length === 0) {
    problems.push('체크포인트가 없습니다. 지도를 클릭해 추가하세요.')
  }
  if (mission.assignments.length === 0) {
    problems.push('배정된 순찰체가 없습니다.')
  }

  const outside = mission.checkpoints.filter(
    (checkpoint) => !isInsideArea(checkpoint.position, area),
  )
  if (outside.length > 0) {
    problems.push(
      `순찰구역 밖 체크포인트: ${outside.map((checkpoint) => checkpoint.name).join(', ')}`,
    )
  }
  if (new Date(mission.windowEnd) <= new Date(mission.windowStart)) {
    problems.push('운용 시간대의 종료 시각이 시작 시각보다 앞섭니다.')
  }

  return { ready: problems.length === 0, problems }
}

/** Units already committed to an overlapping window on another mission. */
export function conflictingUnits(missions: readonly Mission[], candidate: Mission): string[] {
  const start = new Date(candidate.windowStart).getTime()
  const end = new Date(candidate.windowEnd).getTime()

  const busy = new Set<string>()
  for (const mission of missions) {
    if (mission.id === candidate.id) continue
    if (mission.status === 'COMPLETED' || mission.status === 'ABORTED') continue

    const otherStart = new Date(mission.windowStart).getTime()
    const otherEnd = new Date(mission.windowEnd).getTime()
    if (otherStart >= end || otherEnd <= start) continue

    for (const assignment of mission.assignments) busy.add(assignment.callsign)
  }

  return candidate.assignments
    .map((assignment) => assignment.callsign)
    .filter((callsign) => busy.has(callsign))
}
