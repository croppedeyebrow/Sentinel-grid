import { useQuery } from '@tanstack/react-query'
import { statusLabel, type PatrolUnit } from '../../entities/patrol-unit/model'
import { getOperationalPicture } from '../../shared/api/operationalPicture'
import { StatusBadge } from '../../shared/ui/StatusBadge'
import { PatrolMap } from '../../widgets/patrol-map/PatrolMap'

const valueOrMissing = (value: number | null, suffix: string) => value === null ? '누락' : `${value}${suffix}`

function UnitCard({ unit }: { unit: PatrolUnit }) {
  return (
    <article className="unit-card">
      <div className="unit-card__heading">
        <div><p>{unit.callsign}</p><h2>{unit.displayName}</h2></div>
        <StatusBadge status={unit.status} />
      </div>
      <dl className="unit-card__metrics">
        <div><dt>배터리</dt><dd>{valueOrMissing(unit.batteryLevel, '%')}</dd></div>
        <div><dt>링크</dt><dd>{valueOrMissing(unit.linkQuality, '%')}</dd></div>
      </dl>
      <p className="unit-card__detail">
        {unit.latitude === null ? '현재 위치 없음' : `${unit.latitude.toFixed(4)}, ${unit.longitude?.toFixed(4)}`}
      </p>
    </article>
  )
}

export function OperationalPicturePage() {
  const query = useQuery({ queryKey: ['operational-picture'], queryFn: getOperationalPicture })
  if (query.isPending) return <main className="state-panel">운영상황도를 불러오는 중입니다.</main>
  if (query.isError) {
    return <main className="state-panel state-panel--error"><h1>Mission API에 연결할 수 없습니다.</h1><p>{query.error.message}</p></main>
  }

  const summary = query.data.units.reduce(
    (counts, unit) => ({ ...counts, [unit.status]: counts[unit.status] + 1 }),
    { NORMAL: 0, STALE: 0, MISSING: 0 },
  )

  return (
    <main className="shell">
      <header className="topbar">
        <div><p className="eyebrow">SENTINELGRID / LIVE</p><h1>Operational Picture</h1></div>
        <p className="timestamp">Snapshot {new Date(query.data.generatedAt).toLocaleTimeString('ko-KR')}</p>
      </header>
      <section className="summary" aria-label="상태 요약">
        {(['NORMAL', 'STALE', 'MISSING'] as const).map((status) => (
          <div key={status} className={`summary__item summary__item--${status.toLowerCase()}`}>
            <span>{statusLabel[status]}</span><strong>{summary[status]}</strong>
          </div>
        ))}
      </section>
      <section className="workspace">
        <PatrolMap units={query.data.units} />
        <aside className="unit-list" aria-label="순찰체 목록">
          {query.data.units.map((unit) => <UnitCard key={unit.id} unit={unit} />)}
        </aside>
      </section>
    </main>
  )
}

