import { useState } from 'react'
import { abortReasonSchema, type AbortReason, type Mission } from '../../entities/mission/model'
import type { PatrolArea } from '../../entities/patrol-unit/model'
import {
  abortReasonLabel,
  checkApprovalReadiness,
  checkpointStateLabel,
  missionStatusLabel,
  readCoverage,
  transitionLabel,
} from '../../entities/mission/uiModel'

interface MissionDetailProps {
  mission: Mission
  area: PatrolArea | null
  units: readonly { id: string; callsign: string }[]
  busy: boolean
  addingCheckpoints: boolean
  onTransition: (transition: Mission['allowedTransitions'][number], reason?: AbortReason) => void
  onToggleUnit: (unitId: string, assigned: boolean) => void
  onRemoveCheckpoint: (checkpointId: string) => void
  onToggleCheckpointPlacing: () => void
}

function formatWindow(mission: Mission): string {
  const start = new Date(mission.window.start)
  const end = new Date(mission.window.end)
  const time = (value: Date) =>
    value.toLocaleTimeString('ko-KR', { hour: '2-digit', minute: '2-digit' })
  return `${start.toLocaleDateString('ko-KR')} ${time(start)}–${time(end)}`
}

function CoverageBar({ mission }: { mission: Mission }) {
  const reading = readCoverage(mission.coverage)

  return (
    <div className="coverage">
      <div className="coverage__bar" role="img" aria-label={reading.label}>
        <span className="coverage__segment coverage__segment--visited" style={{ width: `${reading.visitedPercent}%` }} />
        <span className="coverage__segment coverage__segment--missed" style={{ width: `${reading.missedPercent}%` }} />
        <span className="coverage__segment coverage__segment--unknown" style={{ width: `${reading.unknownPercent}%` }} />
      </div>
      <p className="coverage__label">{reading.label}</p>
      <p className="coverage__note">
        판단 불가는 통과로도 미순찰로도 세지 않습니다. 근거 관측이 신뢰할 수 없으면 통과를
        인정하지 않습니다.
      </p>
    </div>
  )
}

export function MissionDetail({
  mission,
  area,
  units,
  busy,
  addingCheckpoints,
  onTransition,
  onToggleUnit,
  onRemoveCheckpoint,
  onToggleCheckpointPlacing,
}: MissionDetailProps) {
  const [reason, setReason] = useState<AbortReason | ''>('')
  // The server states which transitions it will accept; the console offers
  // exactly those rather than deriving its own list.
  const transitions = mission.allowedTransitions
  const readiness = checkApprovalReadiness(mission, area)
  const editable = mission.status === 'DRAFT'

  return (
    <section className="mission-detail" aria-label={mission.name}>
      <header>
        <div>
          <p className="eyebrow">{formatWindow(mission)}</p>
          <h2>{mission.name}</h2>
        </div>
        <span className={`mission-chip mission-chip--${mission.status.toLowerCase()}`}>
          {missionStatusLabel[mission.status]}
        </span>
      </header>

      <CoverageBar mission={mission} />

      <dl className="mission-meta">
        <div>
          <dt>구역</dt>
          <dd>{mission.patrolArea.name}</dd>
        </div>
        <div>
          <dt>경로</dt>
          <dd>{mission.patrolRoute?.name ?? '지정 없음'}</dd>
        </div>
        <div>
          <dt>버전</dt>
          <dd>
            v{mission.version} · 정책 v{mission.policyVersion}
          </dd>
        </div>
        <div>
          <dt>배정</dt>
          <dd>
            {mission.assignments.length === 0
              ? '없음'
              : mission.assignments.map((assignment) => assignment.callsign).join(', ')}
          </dd>
        </div>
        {mission.abort === null ? null : (
          <div>
            <dt>중단 사유</dt>
            <dd>
              {abortReasonLabel[mission.abort.reason]} ·{' '}
              {new Date(mission.abort.at).toLocaleTimeString('ko-KR')}
            </dd>
          </div>
        )}
      </dl>

      <h3>체크포인트</h3>
      {mission.checkpoints.length === 0 ? (
        <p className="planner__empty">체크포인트가 없습니다.</p>
      ) : (
        <ol className="checkpoint-list">
          {mission.checkpoints.map((checkpoint) => (
            <li key={checkpoint.id} className={`checkpoint-list__item--${checkpoint.state.toLowerCase()}`}>
              <span className="checkpoint-list__name">
                {checkpoint.sequence}. {checkpoint.name}
              </span>
              <span className="checkpoint-list__state">{checkpointStateLabel[checkpoint.state]}</span>
              <span className="checkpoint-list__evidence">
                {checkpoint.creditedObservedAt === null
                  ? '근거 관측 없음'
                  : `근거 ${new Date(checkpoint.creditedObservedAt).toLocaleTimeString('ko-KR')}`}
              </span>
              {editable ? (
                <button
                  type="button"
                  className="checkpoint-list__remove"
                  aria-label={`${checkpoint.name} 삭제`}
                  disabled={busy}
                  onClick={() => onRemoveCheckpoint(checkpoint.id)}
                >
                  삭제
                </button>
              ) : null}
            </li>
          ))}
        </ol>
      )}

      {editable ? (
        <>
          <button
            type="button"
            className={`placing-toggle${addingCheckpoints ? ' placing-toggle--on' : ''}`}
            onClick={onToggleCheckpointPlacing}
          >
            {addingCheckpoints ? '지도 클릭 추가 중단' : '지도 클릭으로 체크포인트 추가'}
          </button>

          <h3>순찰체 배정</h3>
          <ul className="planner__units">
            {units.map((unit) => {
              const assigned = mission.assignments.some(
                (assignment) => assignment.unitId === unit.id,
              )
              return (
                <li key={unit.id}>
                  <label>
                    <input
                      type="checkbox"
                      checked={assigned}
                      disabled={busy}
                      onChange={() => onToggleUnit(unit.id, assigned)}
                    />
                    {unit.callsign}
                  </label>
                </li>
              )
            })}
          </ul>
        </>
      ) : null}

      {mission.status === 'DRAFT' && !readiness.ready ? (
        <ul className="mission-problems" aria-label="승인 전 확인 사항">
          {readiness.problems.map((problem) => (
            <li key={problem}>{problem}</li>
          ))}
        </ul>
      ) : null}

      {transitions.length === 0 ? (
        <p className="mission-terminal">종료된 임무입니다. 더 이상 전이할 수 없습니다.</p>
      ) : (
        <div className="mission-actions">
          {transitions
            .filter((transition) => transition !== 'abort')
            .map((transition) => (
              <button key={transition} type="button" disabled={busy} onClick={() => onTransition(transition)}>
                {transitionLabel[transition]}
              </button>
            ))}

          {transitions.includes('abort') ? (
            <div className="mission-abort">
              <label>
                중단 사유
                <select
                  value={reason}
                  onChange={(event) => setReason(event.target.value as AbortReason | '')}
                >
                  <option value="">선택하지 않음</option>
                  {abortReasonSchema.options.map((option) => (
                    <option key={option} value={option}>
                      {abortReasonLabel[option]}
                    </option>
                  ))}
                </select>
              </label>
              <button
                type="button"
                className="mission-abort__button"
                disabled={busy}
                onClick={() => onTransition('abort', reason === '' ? undefined : reason)}
              >
                중단
              </button>
            </div>
          ) : null}
        </div>
      )}
    </section>
  )
}
