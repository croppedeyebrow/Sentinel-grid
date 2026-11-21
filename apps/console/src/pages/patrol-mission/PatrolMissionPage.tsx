import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import type { AbortReason, Mission, MissionTransition } from '../../entities/mission/model'
import { missionStatusLabel, readCoverage, refusalMessage } from '../../entities/mission/uiModel'
import type { PatrolArea, Position } from '../../entities/patrol-unit/model'
import { ApiRefusal } from '../../shared/api/apiError'
import {
  addCheckpoint,
  assignUnit,
  createMission,
  getMissions,
  removeCheckpoint,
  transitionMission,
  unassignUnit,
} from '../../shared/api/missions'
import { getOperationalPicture } from '../../shared/api/operationalPicture'
import { MissionMap } from '../../widgets/mission-map/MissionMap'
import { MissionDetail } from './MissionDetail'
import { emptyPlanner, MissionPlanner, type PlannerDraft } from './MissionPlanner'

/**
 * Patrol Mission screens, wired to the real API.
 *
 * Server state (missions, site geometry) lives in TanStack Query; the planner
 * draft, the selection and the last notice are screen state. Every refusal the
 * server returns is shown by its contract code, so the operator reads why a
 * request was refused rather than a generic failure.
 */

interface Notice {
  kind: 'ok' | 'refusal'
  text: string
}

interface MutationInput {
  task: () => Promise<Mission>
  success?: string
}

function describe(error: unknown): string {
  if (error instanceof ApiRefusal) return refusalMessage(error.code, error.details)
  return error instanceof Error ? error.message : '요청이 실패했습니다.'
}

export function PatrolMissionPage() {
  const picture = useQuery({ queryKey: ['operational-picture'], queryFn: getOperationalPicture })
  const missions = useQuery({ queryKey: ['missions'], queryFn: getMissions })
  const queryClient = useQueryClient()

  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [planner, setPlanner] = useState<PlannerDraft | null>(null)
  const [placing, setPlacing] = useState(false)
  const [notice, setNotice] = useState<Notice | null>(null)

  const action = useMutation({
    mutationFn: (input: MutationInput) => input.task(),
    onSuccess: async (mission, input) => {
      setSelectedId(mission.id)
      setNotice(input.success === undefined ? null : { kind: 'ok', text: input.success })
      await queryClient.invalidateQueries({ queryKey: ['missions'] })
    },
    onError: (error) => setNotice({ kind: 'refusal', text: describe(error) }),
  })

  const run = (task: () => Promise<Mission>, success?: string) => {
    setNotice(null)
    action.mutate({ task, success })
  }

  if (picture.isPending || missions.isPending) {
    return (
      <main className="state-panel" role="status">
        순찰 임무를 불러오는 중입니다.
      </main>
    )
  }

  if (picture.isError || missions.isError) {
    const failure = picture.error ?? missions.error
    return (
      <main className="state-panel state-panel--error" role="alert">
        <h1>Mission API에 연결할 수 없습니다.</h1>
        <p>{describe(failure)}</p>
        <button
          type="button"
          onClick={() => {
            void picture.refetch()
            void missions.refetch()
          }}
        >
          다시 시도
        </button>
      </main>
    )
  }

  const site = picture.data.site
  const units = picture.data.units.map((unit) => ({ id: unit.id, callsign: unit.callsign }))
  const selected = missions.data.find((mission) => mission.id === selectedId) ?? null

  const area: PatrolArea | null =
    picture.data.patrolAreas.find((candidate) => candidate.id === selected?.patrolArea.id) ??
    picture.data.patrolAreas[0] ??
    null
  const route =
    picture.data.patrolRoutes.find((candidate) => candidate.patrolAreaId === area?.id) ?? null

  const placeCheckpoint = (position: Position) => {
    if (planner !== null) {
      setPlanner({
        ...planner,
        checkpoints: [
          ...planner.checkpoints,
          { name: `CP-${planner.checkpoints.length + 1}`, position },
        ],
      })
      return
    }
    if (selected === null || !placing) return

    run(
      () =>
        addCheckpoint(selected.id, {
          name: `CP-${selected.checkpoints.length + 1}`,
          position,
          radiusMeters: 40,
        }),
      '체크포인트를 추가했습니다.',
    )
  }

  const mapCheckpoints =
    planner !== null
      ? planner.checkpoints.map((checkpoint, index) => ({
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

  const transition = (which: MissionTransition, reason?: AbortReason) => {
    if (selected === null) return
    run(
      () =>
        transitionMission({
          missionId: selected.id,
          transition: which,
          // The version the operator last saw: the server refuses a stale one
          // instead of overwriting someone else's outcome.
          version: selected.version,
          reason,
        }),
      which === 'approve'
        ? '승인했습니다. 구역·경로·체크포인트와 정책이 이 임무 버전에 고정됩니다.'
        : undefined,
    )
  }

  return (
    <main className="shell">
      <header className="topbar">
        <div>
          <p className="eyebrow">SENTINELGRID / {site.name}</p>
          <h1>순찰 임무</h1>
          <p className="policy">
            임무 {missions.data.length}건
            {missions.isFetching ? <span className="timestamp__refreshing"> · 갱신 중</span> : null}
          </p>
        </div>
        <div className="topbar__meta">
          <Link className="nav-link" to="/">
            운영상황도로 이동
          </Link>
        </div>
      </header>

      {notice === null ? null : (
        <p
          className={`notice${notice.kind === 'refusal' ? ' notice--refusal' : ''}`}
          role={notice.kind === 'refusal' ? 'alert' : 'status'}
        >
          {notice.text}
        </p>
      )}

      <section className="mission-workspace">
        <aside className="mission-list" aria-label="임무 목록">
          <button
            type="button"
            className="mission-list__new"
            onClick={() => {
              setPlanner(emptyPlanner(new Date()))
              setSelectedId(null)
              setPlacing(false)
              setNotice(null)
            }}
          >
            새 임무 계획
          </button>
          {missions.data.length === 0 ? (
            <p className="planner__empty">등록된 임무가 없습니다.</p>
          ) : (
            missions.data.map((mission) => (
              <button
                key={mission.id}
                type="button"
                className={`mission-list__item${mission.id === selectedId ? ' mission-list__item--selected' : ''}`}
                onClick={() => {
                  setSelectedId(mission.id)
                  setPlanner(null)
                  setPlacing(false)
                  setNotice(null)
                }}
              >
                <span className="mission-list__name">{mission.name}</span>
                <span className={`mission-chip mission-chip--${mission.status.toLowerCase()}`}>
                  {missionStatusLabel[mission.status]}
                </span>
                <span className="mission-list__coverage">{readCoverage(mission.coverage).label}</span>
              </button>
            ))
          )}
        </aside>

        {area === null ? (
          <section className="state-panel state-panel--empty" aria-label="순찰구역 없음">
            <h2>순찰구역이 없습니다.</h2>
            <p>구역이 등록되면 지도에 표시됩니다.</p>
          </section>
        ) : (
          <MissionMap
            area={area}
            route={route}
            checkpoints={mapCheckpoints}
            planning={planner !== null || placing}
            onPlaceCheckpoint={placeCheckpoint}
          />
        )}

        {planner !== null ? (
          <MissionPlanner
            draft={planner}
            area={area}
            units={units}
            saving={action.isPending}
            onChange={setPlanner}
            onCancel={() => setPlanner(null)}
            onSave={() => {
              if (area === null) return
              run(
                () =>
                  createMission({
                    name: planner.name.trim() === '' ? '이름 없는 임무' : planner.name.trim(),
                    patrolAreaId: area.id,
                    patrolRouteId: route?.id ?? null,
                    window: { start: planner.windowStart, end: planner.windowEnd },
                    checkpoints: planner.checkpoints.map((checkpoint) => ({
                      name: checkpoint.name,
                      position: checkpoint.position,
                      radiusMeters: 40,
                    })),
                    unitIds: planner.assignedUnitIds,
                  }),
                '초안으로 저장했습니다. 승인 전에는 실행할 수 없습니다.',
              )
              setPlanner(null)
            }}
          />
        ) : selected === null ? (
          <section className="state-panel state-panel--empty" aria-label="임무 상세">
            <h2>선택된 임무가 없습니다.</h2>
            <p>목록에서 임무를 선택하거나 새 임무를 계획하세요.</p>
          </section>
        ) : (
          <MissionDetail
            mission={selected}
            area={area}
            units={units}
            busy={action.isPending}
            addingCheckpoints={placing}
            onTransition={transition}
            onToggleUnit={(unitId, assigned) =>
              run(
                () =>
                  assigned ? unassignUnit(selected.id, unitId) : assignUnit(selected.id, unitId),
                assigned ? '배정을 해제했습니다.' : '순찰체를 배정했습니다.',
              )
            }
            onRemoveCheckpoint={(checkpointId) =>
              run(() => removeCheckpoint(selected.id, checkpointId), '체크포인트를 삭제했습니다.')
            }
            onToggleCheckpointPlacing={() => setPlacing((current) => !current)}
          />
        )}
      </section>
    </main>
  )
}
