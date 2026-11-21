import type { PatrolArea } from '../patrol-unit/model'
import { isInsideArea } from '../../shared/geo/polygon'
import type {
  AbortReason,
  ApiError,
  CheckpointState,
  ErrorCode,
  Mission,
  MissionCoverage,
  MissionStatus,
  MissionTransition,
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

export const transitionLabel: Record<MissionTransition, string> = {
  approve: '승인',
  start: '시작',
  complete: '완료',
  abort: '중단',
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
 * Preconditions the server puts on approval, checked locally so the planner is
 * told what is missing before sending the request. The server re-checks every
 * one of them and remains the authority.
 */
export function checkApprovalReadiness(mission: Mission, area: PatrolArea | null): ApprovalReadiness {
  const problems: string[] = []

  if (mission.checkpoints.length === 0) {
    problems.push('체크포인트가 없습니다. 지도를 클릭해 추가하세요.')
  }
  if (mission.assignments.length === 0) {
    problems.push('배정된 순찰체가 없습니다.')
  }
  if (area !== null) {
    const outside = mission.checkpoints.filter(
      (checkpoint) => !isInsideArea(checkpoint.position, area),
    )
    if (outside.length > 0) {
      problems.push(
        `순찰구역 밖 체크포인트: ${outside.map((checkpoint) => checkpoint.name).join(', ')}`,
      )
    }
  }
  if (new Date(mission.window.end) <= new Date(mission.window.start)) {
    problems.push('운용 시간대의 종료 시각이 시작 시각보다 앞섭니다.')
  }

  return { ready: problems.length === 0, problems }
}

const refusalText: Record<ErrorCode, string> = {
  INVALID_REQUEST: '요청 형식이 올바르지 않습니다.',
  MISSION_NOT_FOUND: '임무를 찾을 수 없습니다. 목록을 새로 불러오세요.',
  CHECKPOINT_NOT_FOUND: '체크포인트를 찾을 수 없습니다.',
  PATROL_AREA_NOT_FOUND: '순찰구역을 찾을 수 없습니다.',
  UNIT_NOT_FOUND: '순찰체를 찾을 수 없습니다.',
  NO_CHECKPOINTS: '체크포인트가 없어 승인할 수 없습니다.',
  NO_ASSIGNMENT: '배정된 순찰체가 없어 승인할 수 없습니다.',
  INVALID_WINDOW: '운용 시간대가 올바르지 않습니다.',
  CHECKPOINT_OUTSIDE_AREA: '순찰구역 밖 체크포인트가 있습니다.',
  ASSIGNMENT_CONFLICT: '순찰체가 겹치는 시간대에 이미 배정돼 있습니다.',
  TRANSITION_NOT_ALLOWED: '현재 상태에서는 요청한 전이를 할 수 없습니다.',
  STALE_VERSION: '임무가 그 사이에 변경됐습니다. 최신 상태를 확인한 뒤 다시 시도하세요.',
  ABORT_REASON_REQUIRED: '중단 사유를 선택해야 합니다.',
  UNKNOWN_ABORT_REASON: '알 수 없는 중단 사유입니다.',
  NOT_DRAFT: '승인된 계획은 더 이상 수정할 수 없습니다.',
  NOT_PROVISIONED: 'Site Profile이 아직 설정되지 않았습니다.',
  PROJECTION_UNAVAILABLE: '임무 저장소에 연결할 수 없습니다.',
}

/**
 * Explains a refusal from its code and the facts the server attached. The
 * server's own sentence is not shown: the contract says clients key their
 * wording off the code.
 */
export function refusalMessage(
  code: ErrorCode | 'UNKNOWN',
  details?: ApiError['details'],
): string {
  const base = code === 'UNKNOWN' ? '요청이 거부됐습니다.' : refusalText[code]

  if (code === 'ASSIGNMENT_CONFLICT' && details?.callsigns?.length) {
    return `${details.callsigns.join(', ')}가 겹치는 시간대에 이미 배정돼 있습니다.`
  }
  if (code === 'CHECKPOINT_OUTSIDE_AREA' && details?.checkpointName) {
    return `순찰구역 밖 체크포인트: ${details.checkpointName}`
  }
  if (code === 'TRANSITION_NOT_ALLOWED' && details?.status) {
    return `${missionStatusLabel[details.status]} 상태에서는 요청한 전이를 할 수 없습니다.`
  }
  if (code === 'NOT_DRAFT' && details?.status) {
    return `${missionStatusLabel[details.status]} 상태에서는 계획을 수정할 수 없습니다.`
  }
  return base
}
