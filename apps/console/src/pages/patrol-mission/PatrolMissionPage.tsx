import { useReducer, useState } from 'react'
import { Link } from 'react-router-dom'
import type { AbortReason, Mission } from '../../entities/mission/model'
import { abortReasonSchema } from '../../entities/mission/model'
import { mockPatrolArea, mockPatrolRoute } from '../../entities/mission/mockMissions'
import {
  abortReasonLabel,
  allowedTransitions,
  checkApprovalReadiness,
  checkpointStateLabel,
  missionStatusLabel,
  readCoverage,
  transitionLabel,
} from '../../entities/mission/uiModel'
import { MissionMap } from '../../widgets/mission-map/MissionMap'
import { MissionPlanner } from './MissionPlanner'
import {
  initialWorkspace,
  missionWorkspaceReducer,
  type MissionWorkspaceAction,
  type MissionWorkspaceState,
} from './missionWorkspace'

/**
 * Mock mission screens — step 2 of the slice order. No mission API exists yet;
 * the transitions run against local state so the screens, refusals and
 * coverage readings can be reviewed before the domain is written.
 */

function formatWindow(mission: Mission): string {
  const start = new Date(mission.windowStart)
  const end = new Date(mission.windowEnd)
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
        판단 불가는 통과로도 미순찰로도 세지 않습니다. 근거 관측이 신선하지 않으면 통과를
        인정하지 않습니다.
      </p>
    </div>
  )
}

function MissionDetail({
  mission,
  dispatch,
}: {
  mission: Mission
  dispatch: (action: MissionWorkspaceAction) => void
}) {
  const [reason, setReason] = useState<AbortReason | ''>('')
  const transitions = allowedTransitions(mission.status)
  const readiness = checkApprovalReadiness(mission, mockPatrolArea)

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
          <dt>배정</dt>
          <dd>
            {mission.assignments.length === 0
              ? '없음'
              : mission.assignments.map((assignment) => assignment.callsign).join(', ')}
          </dd>
        </div>
        <div>
          <dt>버전</dt>
          <dd>v{mission.version}</dd>
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
          </li>
        ))}
      </ol>

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
              <button
                key={transition}
                type="button"
                onClick={() => dispatch({ type: 'transition', id: mission.id, transition })}
              >
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
                onClick={() =>
                  dispatch({
                    type: 'transition',
                    id: mission.id,
                    transition: 'abort',
                    reason: reason === '' ? undefined : reason,
                  })
                }
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

export function PatrolMissionPage() {
  const [state, dispatch] = useReducer(
    (current: MissionWorkspaceState, action: MissionWorkspaceAction) =>
      missionWorkspaceReducer(current, action, { area: mockPatrolArea, now: new Date() }),
    initialWorkspace,
  )

  const selected = state.missions.find((mission) => mission.id === state.selectedId) ?? null
  const mapCheckpoints =
    state.planner !== null
      ? state.planner.checkpoints.map((checkpoint, index) => ({
          sequence: index + 1,
          name: checkpoint.name,
          position: checkpoint.position,
          state: 'PLANNED' as const,
        }))
      : (selected?.checkpoints ?? []).map((checkpoint) => ({
          sequence: checkpoint.sequence,
          name: checkpoint.name,
          position: checkpoint.position,
          state: checkpoint.state,
        }))

  return (
    <main className="shell">
      <header className="topbar">
        <div>
          <p className="eyebrow">SENTINELGRID / PATROL MISSION</p>
          <h1>순찰 임무</h1>
          <p className="policy">Mock 화면입니다. 임무 API 연동 전 설계 확인용입니다.</p>
        </div>
        <div className="topbar__meta">
          <Link className="nav-link" to="/">
            운영상황도로 이동
          </Link>
        </div>
      </header>

      {state.notice === null ? null : (
        <p className="notice" role="status">
          {state.notice}
        </p>
      )}

      <section className="mission-workspace">
        <aside className="mission-list" aria-label="임무 목록">
          <button
            type="button"
            className="mission-list__new"
            onClick={() => dispatch({ type: 'planning/start' })}
          >
            새 임무 계획
          </button>
          {state.missions.map((mission) => (
            <button
              key={mission.id}
              type="button"
              className={`mission-list__item${mission.id === state.selectedId ? ' mission-list__item--selected' : ''}`}
              onClick={() => dispatch({ type: 'select', id: mission.id })}
            >
              <span className="mission-list__name">{mission.name}</span>
              <span className={`mission-chip mission-chip--${mission.status.toLowerCase()}`}>
                {missionStatusLabel[mission.status]}
              </span>
              <span className="mission-list__coverage">{readCoverage(mission.coverage).label}</span>
            </button>
          ))}
        </aside>

        <MissionMap
          area={mockPatrolArea}
          route={mockPatrolRoute}
          checkpoints={mapCheckpoints}
          planning={state.planner !== null}
          onPlaceCheckpoint={(position) => dispatch({ type: 'planning/placeCheckpoint', position })}
        />

        {state.planner !== null ? (
          <MissionPlanner draft={state.planner} area={mockPatrolArea} dispatch={dispatch} />
        ) : selected === null ? (
          <section className="state-panel state-panel--empty" aria-label="임무 상세">
            <h2>선택된 임무가 없습니다.</h2>
            <p>목록에서 임무를 선택하거나 새 임무를 계획하세요.</p>
          </section>
        ) : (
          <MissionDetail mission={selected} dispatch={dispatch} />
        )}
      </section>
    </main>
  )
}
