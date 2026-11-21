import { fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, it, vi } from 'vitest'
import type { Position } from '../entities/patrol-unit/model'
import { PatrolMissionPage } from '../pages/patrol-mission/PatrolMissionPage'

// The map needs WebGL, so it is replaced by a stub that exposes the one
// interaction the planning flow depends on: placing a checkpoint by clicking.
vi.mock('../widgets/mission-map/MissionMap', () => ({
  MissionMap: ({
    planning,
    checkpoints,
    onPlaceCheckpoint,
  }: {
    planning: boolean
    checkpoints: readonly { sequence: number }[]
    onPlaceCheckpoint?: (position: Position) => void
  }) => (
    <div data-testid="mission-map" data-planning={String(planning)}>
      <span data-testid="map-checkpoint-count">{checkpoints.length}</span>
      <button
        type="button"
        onClick={() => onPlaceCheckpoint?.({ latitude: 37.565, longitude: 126.978 })}
      >
        지도 클릭
      </button>
    </div>
  ),
}))

function renderPage() {
  return render(
    <MemoryRouter>
      <PatrolMissionPage />
    </MemoryRouter>,
  )
}

describe('PatrolMissionPage', () => {
  it('shows the mission list with each mission state and coverage reading', () => {
    renderPage()

    const list = within(screen.getByLabelText('임무 목록'))
    expect(list.getByText('하천변 정기 순찰 (오전)')).toBeInTheDocument()
    expect(list.getByText('진행 중')).toBeInTheDocument()
    expect(list.getByText('초안')).toBeInTheDocument()
    expect(list.getByText('중단')).toBeInTheDocument()
    // A running mission reports no misses: the unit may still get there.
    expect(list.getByText('통과 2 · 미순찰 0 · 판단 불가 0 / 5')).toBeInTheDocument()
    // The link was lost on the aborted mission, so nothing is called missed.
    expect(list.getByText('통과 0 · 미순찰 0 · 판단 불가 3 / 3')).toBeInTheDocument()
  })

  it('reads coverage and evidence on the selected mission', () => {
    renderPage()

    const detail = within(screen.getByLabelText('하천변 정기 순찰 (오전)'))
    expect(detail.getByText('통과 2 · 미순찰 0 · 판단 불가 0 / 5')).toBeInTheDocument()
    expect(detail.getAllByText('통과')).toHaveLength(2)
    expect(detail.getAllByText('미실행')).toHaveLength(3)
    expect(detail.getAllByText('근거 관측 없음')).toHaveLength(3)
  })

  it('places checkpoints by clicking the map while planning', () => {
    renderPage()

    fireEvent.click(screen.getByRole('button', { name: '새 임무 계획' }))
    expect(screen.getByTestId('mission-map')).toHaveAttribute('data-planning', 'true')

    fireEvent.click(screen.getByRole('button', { name: '지도 클릭' }))
    fireEvent.click(screen.getByRole('button', { name: '지도 클릭' }))

    const planner = within(screen.getByLabelText('임무 계획'))
    expect(planner.getByText(/CP-1/)).toBeInTheDocument()
    expect(planner.getByText(/CP-2/)).toBeInTheDocument()
    expect(screen.getByTestId('map-checkpoint-count')).toHaveTextContent('2')
  })

  it('saves a draft and says it cannot run before approval', () => {
    renderPage()

    fireEvent.click(screen.getByRole('button', { name: '새 임무 계획' }))
    fireEvent.change(screen.getByLabelText('임무 이름'), { target: { value: '오후 순찰' } })
    fireEvent.click(screen.getByRole('button', { name: '지도 클릭' }))
    fireEvent.click(screen.getByRole('button', { name: '초안 저장' }))

    expect(screen.getByRole('status')).toHaveTextContent('승인 전에는 실행할 수 없습니다')
    expect(within(screen.getByLabelText('오후 순찰')).getByText('초안')).toBeInTheDocument()
  })

  it('refuses to approve a draft that has no assigned unit, and says why', () => {
    renderPage()

    fireEvent.click(screen.getByRole('button', { name: /공사 구역 상황 순찰/ }))
    fireEvent.click(screen.getByRole('button', { name: '승인' }))

    expect(screen.getByRole('status')).toHaveTextContent('배정된 순찰체가 없습니다')
    expect(within(screen.getByLabelText('공사 구역 상황 순찰')).getByText('초안')).toBeInTheDocument()
  })

  it('refuses an abort with no reason selected', () => {
    renderPage()

    const detail = within(screen.getByLabelText('하천변 정기 순찰 (오전)'))
    fireEvent.click(detail.getByRole('button', { name: '중단' }))

    expect(screen.getByRole('status')).toHaveTextContent('중단 사유를 선택해야 합니다')
  })

  it('aborts with a reason and records it', () => {
    renderPage()

    const detail = within(screen.getByLabelText('하천변 정기 순찰 (오전)'))
    fireEvent.change(detail.getByLabelText('중단 사유'), { target: { value: 'BATTERY' } })
    fireEvent.click(detail.getByRole('button', { name: '중단' }))

    const updated = within(screen.getByLabelText('하천변 정기 순찰 (오전)'))
    expect(updated.getByText('중단')).toBeInTheDocument()
    expect(updated.getByText(/배터리 부족/)).toBeInTheDocument()
    expect(updated.getByText(/종료된 임무입니다/)).toBeInTheDocument()
  })

  it('offers no transition on a finished mission', () => {
    renderPage()

    fireEvent.click(screen.getByRole('button', { name: /하천변 정기 순찰 \(야간\)/ }))

    const detail = within(screen.getByLabelText('하천변 정기 순찰 (야간)'))
    expect(detail.getByText(/종료된 임무입니다/)).toBeInTheDocument()
    expect(detail.queryByRole('button', { name: '중단' })).not.toBeInTheDocument()
  })
})
