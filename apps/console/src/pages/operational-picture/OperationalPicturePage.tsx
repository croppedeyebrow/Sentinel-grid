import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import type { UnitStatus } from '../../entities/patrol-unit/model'
import {
  countSilentStreams,
  countUntrustedPositions,
  describePolicy,
  statusLabel,
  statusTone,
  summarizeStatuses,
  toUiIngest,
  toUiPatrolUnit,
  type UiObservation,
  type UiPatrolUnit,
} from '../../entities/patrol-unit/uiModel'
import { getOperationalPicture } from '../../shared/api/operationalPicture'
import { FreshnessChip } from '../../shared/ui/FreshnessChip'
import { StatusBadge } from '../../shared/ui/StatusBadge'
import { PatrolMap } from '../../widgets/patrol-map/PatrolMap'

const statusOrder: UnitStatus[] = ['NORMAL', 'WARNING', 'CRITICAL', 'UNKNOWN']

function Metric({ label, observation }: { label: string; observation: UiObservation }) {
  return (
    <div className={`metric${observation.trusted ? '' : ' metric--untrusted'}`}>
      <dt>{label}</dt>
      <dd>
        <span className="metric__value">{observation.valueLabel}</span>
        <FreshnessChip
          freshness={observation.freshness}
          age={observation.observedAgoLabel ?? observation.receivedAgoLabel}
        />
      </dd>
    </div>
  )
}

function UnitCard({ unit }: { unit: UiPatrolUnit }) {
  return (
    <article className={`unit-card unit-card--${unit.tone}`} aria-label={unit.callsign}>
      <div className="unit-card__heading">
        <div>
          <p className="unit-card__callsign">{unit.callsign}</p>
          <h2>{unit.displayName}</h2>
        </div>
        <StatusBadge status={unit.status} />
      </div>

      <p className="unit-card__explanation">{unit.statusExplanation}</p>

      <dl className="unit-card__metrics">
        <Metric label="배터리" observation={unit.battery} />
        <Metric label="링크" observation={unit.link} />
        <Metric label="위치" observation={unit.position} />
      </dl>

      <div className={`stream stream--${unit.stream.state.toLowerCase()}`}>
        <span className="stream__state">{unit.stream.label}</span>
        <span className="stream__received">
          {unit.stream.lastReceivedAgoLabel === null
            ? '수신 기록 없음'
            : `마지막 수신 ${unit.stream.lastReceivedAgoLabel}`}
        </span>
        {unit.stream.reconnected ? <span className="stream__badge">재접속</span> : null}
        {unit.stream.warning === '' ? null : (
          <span className="stream__warning">{unit.stream.warning}</span>
        )}
      </div>
    </article>
  )
}

export function OperationalPicturePage() {
  const query = useQuery({ queryKey: ['operational-picture'], queryFn: getOperationalPicture })

  if (query.isPending) {
    return (
      <main className="state-panel" role="status">
        운영상황도를 불러오는 중입니다.
      </main>
    )
  }

  if (query.isError) {
    return (
      <main className="state-panel state-panel--error" role="alert">
        <h1>Mission API에 연결할 수 없습니다.</h1>
        <p>{query.error.message}</p>
        <button type="button" onClick={() => void query.refetch()}>
          다시 시도
        </button>
      </main>
    )
  }

  const picture = query.data
  // Ages are measured against the moment the server judged every unit, so the
  // screen cannot disagree with the verdicts it is showing.
  const judgedAt = new Date(picture.generatedAt)
  const units = picture.units.map((unit) => toUiPatrolUnit(unit, picture.site.policy, judgedAt))
  const summary = summarizeStatuses(units)
  const untrustedPositions = countUntrustedPositions(units)
  const silentStreams = countSilentStreams(units)
  const ingest = toUiIngest(picture.ingest, judgedAt)

  return (
    <main className="shell">
      <header className="topbar">
        <div>
          <p className="eyebrow">SENTINELGRID / {picture.site.name}</p>
          <h1>Operational Picture</h1>
          <p className="policy">{describePolicy(picture.site.policy)}</p>
        </div>
        <div className="topbar__meta">
          <p className="timestamp">
            Snapshot {judgedAt.toLocaleTimeString('ko-KR')}
            {query.isFetching ? <span className="timestamp__refreshing"> 갱신 중</span> : null}
          </p>
          <p className="trust-note">
            위치 미신뢰 {untrustedPositions} / {units.length}
          </p>
          <p className="trust-note">
            수신 끊김 {silentStreams} / {units.length}
          </p>
          <p className={`ingest-note ingest-note--${ingest.receiving ? 'ok' : 'broken'}`}>
            {ingest.label}
            {ingest.lastEventAgoLabel !== null ? ` · 최근 수신 ${ingest.lastEventAgoLabel}` : null}
          </p>
          <Link className="nav-link" to="/missions">
            순찰 임무로 이동
          </Link>
        </div>
      </header>

      {ingest.warning === '' ? null : (
        <section className="ingest-banner" role="status" aria-label="수신 경로 경고">
          <strong>{ingest.label}</strong>
          <span>{ingest.warning}</span>
          {silentStreams > 0 ? (
            <span className="ingest-banner__scope">
              현재 {silentStreams} / {units.length}대가 침묵 상태로 보이지만, 그 원인은 순찰체가 아닐 수
              있습니다.
            </span>
          ) : null}
        </section>
      )}

      <section className="summary" aria-label="상태 요약">
        {statusOrder.map((status) => (
          <div key={status} className={`summary__item summary__item--${statusTone[status]}`}>
            <span>{statusLabel[status]}</span>
            <strong>{summary[status]}</strong>
          </div>
        ))}
      </section>

      <section className="workspace">
        <PatrolMap
          site={picture.site}
          patrolAreas={picture.patrolAreas}
          patrolRoutes={picture.patrolRoutes}
          units={units}
        />
        <aside className="unit-list" aria-label="순찰체 목록">
          {units.length === 0 ? (
            <div className="state-panel state-panel--empty" role="status">
              <h2>표시할 순찰체가 없습니다.</h2>
              <p>{picture.site.name}에 배치된 순찰체가 등록되면 이 목록에 나타납니다.</p>
            </div>
          ) : (
            units.map((unit) => <UnitCard key={unit.id} unit={unit} />)
          )}
        </aside>
      </section>
    </main>
  )
}
