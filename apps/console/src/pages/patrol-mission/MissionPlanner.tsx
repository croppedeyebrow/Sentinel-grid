import type { PatrolArea, Position } from '../../entities/patrol-unit/model'
import { isInsideArea } from '../../shared/geo/polygon'

/**
 * Local form state for a new plan. It is UI state, not server state: nothing
 * exists on the server until the planner saves the draft.
 */
export interface PlannerDraft {
  name: string
  windowStart: string
  windowEnd: string
  checkpoints: { name: string; position: Position }[]
  assignedUnitIds: string[]
}

export function emptyPlanner(now: Date): PlannerDraft {
  const start = new Date(now.getTime() + 30 * 60 * 1000)
  const end = new Date(start.getTime() + 2 * 60 * 60 * 1000)
  return {
    name: '',
    windowStart: start.toISOString(),
    windowEnd: end.toISOString(),
    checkpoints: [],
    assignedUnitIds: [],
  }
}

interface MissionPlannerProps {
  draft: PlannerDraft
  area: PatrolArea | null
  units: readonly { id: string; callsign: string }[]
  saving: boolean
  onChange: (draft: PlannerDraft) => void
  onSave: () => void
  onCancel: () => void
}

function toLocalInput(iso: string): string {
  return iso.slice(0, 16)
}

export function MissionPlanner({
  draft,
  area,
  units,
  saving,
  onChange,
  onSave,
  onCancel,
}: MissionPlannerProps) {
  const outside =
    area === null
      ? []
      : draft.checkpoints.filter((checkpoint) => !isInsideArea(checkpoint.position, area))

  return (
    <section className="planner" aria-label="임무 계획">
      <h2>새 임무 계획</h2>

      <label className="planner__field">
        임무 이름
        <input
          type="text"
          value={draft.name}
          placeholder="예: 하천변 정기 순찰 (오후)"
          onChange={(event) => onChange({ ...draft, name: event.target.value })}
        />
      </label>

      <div className="planner__window">
        <label className="planner__field">
          운용 시작
          <input
            type="datetime-local"
            value={toLocalInput(draft.windowStart)}
            onChange={(event) =>
              onChange({ ...draft, windowStart: new Date(event.target.value).toISOString() })
            }
          />
        </label>
        <label className="planner__field">
          운용 종료
          <input
            type="datetime-local"
            value={toLocalInput(draft.windowEnd)}
            onChange={(event) =>
              onChange({ ...draft, windowEnd: new Date(event.target.value).toISOString() })
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
            const inside = area === null || isInsideArea(checkpoint.position, area)
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
                  onClick={() =>
                    onChange({
                      ...draft,
                      checkpoints: draft.checkpoints.filter((_, position) => position !== index),
                    })
                  }
                >
                  삭제
                </button>
              </li>
            )
          })}
        </ol>
      )}
      {outside.length > 0 ? (
        <p className="planner__warning" role="status">
          구역 밖 체크포인트가 {outside.length}개 있습니다. 승인 시 서버가 거부합니다.
        </p>
      ) : null}

      <h3>순찰체 배정</h3>
      <ul className="planner__units">
        {units.map((unit) => (
          <li key={unit.id}>
            <label>
              <input
                type="checkbox"
                checked={draft.assignedUnitIds.includes(unit.id)}
                onChange={() =>
                  onChange({
                    ...draft,
                    assignedUnitIds: draft.assignedUnitIds.includes(unit.id)
                      ? draft.assignedUnitIds.filter((id) => id !== unit.id)
                      : [...draft.assignedUnitIds, unit.id],
                  })
                }
              />
              {unit.callsign}
            </label>
          </li>
        ))}
      </ul>

      <div className="planner__actions">
        <button type="button" onClick={onSave} disabled={saving}>
          {saving ? '저장 중' : '초안 저장'}
        </button>
        <button type="button" className="planner__cancel" onClick={onCancel}>
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
