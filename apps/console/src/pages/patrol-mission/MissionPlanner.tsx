import { mockUnits } from '../../entities/mission/mockMissions'
import { isInsideArea } from '../../shared/geo/polygon'
import type { PatrolArea } from '../../entities/patrol-unit/model'
import type { MissionWorkspaceAction, PlannerDraft } from './missionWorkspace'

interface MissionPlannerProps {
  draft: PlannerDraft
  area: PatrolArea
  dispatch: (action: MissionWorkspaceAction) => void
}

function toLocalInput(iso: string): string {
  // datetime-local wants a value without the timezone suffix.
  return iso.slice(0, 16)
}

export function MissionPlanner({ draft, area, dispatch }: MissionPlannerProps) {
  const outsideCount = draft.checkpoints.filter(
    (checkpoint) => !isInsideArea(checkpoint.position, area),
  ).length

  return (
    <section className="planner" aria-label="임무 계획">
      <h2>새 임무 계획</h2>

      <label className="planner__field">
        임무 이름
        <input
          type="text"
          value={draft.name}
          placeholder="예: 하천변 정기 순찰 (오후)"
          onChange={(event) => dispatch({ type: 'planning/name', name: event.target.value })}
        />
      </label>

      <div className="planner__window">
        <label className="planner__field">
          운용 시작
          <input
            type="datetime-local"
            value={toLocalInput(draft.windowStart)}
            onChange={(event) =>
              dispatch({
                type: 'planning/window',
                windowStart: new Date(event.target.value).toISOString(),
                windowEnd: draft.windowEnd,
              })
            }
          />
        </label>
        <label className="planner__field">
          운용 종료
          <input
            type="datetime-local"
            value={toLocalInput(draft.windowEnd)}
            onChange={(event) =>
              dispatch({
                type: 'planning/window',
                windowStart: draft.windowStart,
                windowEnd: new Date(event.target.value).toISOString(),
              })
            }
          />
        </label>
      </div>

      <h3>
        체크포인트 <span className="planner__count">{draft.checkpoints.length}</span>
      </h3>
      {draft.checkpoints.length === 0 ? (
        <p className="planner__empty">지도를 클릭해 체크포인트를 추가하세요.</p>
      ) : (
        <ol className="planner__checkpoints">
          {draft.checkpoints.map((checkpoint, index) => {
            const inside = isInsideArea(checkpoint.position, area)
            return (
              <li key={`${checkpoint.name}-${index}`} className={inside ? '' : 'planner__outside'}>
                <span>
                  {checkpoint.name} · {checkpoint.position.latitude.toFixed(4)},{' '}
                  {checkpoint.position.longitude.toFixed(4)}
                </span>
                {inside ? null : <em>구역 밖</em>}
                <button
                  type="button"
                  aria-label={`${checkpoint.name} 삭제`}
                  onClick={() => dispatch({ type: 'planning/removeCheckpoint', index })}
                >
                  삭제
                </button>
              </li>
            )
          })}
        </ol>
      )}
      {outsideCount > 0 ? (
        <p className="planner__warning" role="status">
          구역 밖 체크포인트가 {outsideCount}개 있습니다. 승인 시 서버가 거부합니다.
        </p>
      ) : null}

      <h3>순찰체 배정</h3>
      <ul className="planner__units">
        {mockUnits.map((unit) => (
          <li key={unit.unitId}>
            <label>
              <input
                type="checkbox"
                checked={draft.assignedCallsigns.includes(unit.callsign)}
                onChange={() =>
                  dispatch({ type: 'planning/toggleUnit', callsign: unit.callsign })
                }
              />
              {unit.callsign}
            </label>
          </li>
        ))}
      </ul>

      <div className="planner__actions">
        <button type="button" onClick={() => dispatch({ type: 'planning/create' })}>
          초안 저장
        </button>
        <button
          type="button"
          className="planner__cancel"
          onClick={() => dispatch({ type: 'planning/cancel' })}
        >
          취소
        </button>
      </div>
      <p className="planner__note">
        초안은 승인 전까지 실행할 수 없습니다. 승인 시 구역·경로·체크포인트와 Site Profile
        정책이 이 임무 버전에 고정됩니다.
      </p>
    </section>
  )
}
