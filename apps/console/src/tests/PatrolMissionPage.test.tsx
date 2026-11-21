import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { Position } from '../entities/patrol-unit/model'
import { PatrolMissionPage } from '../pages/patrol-mission/PatrolMissionPage'
import { readExample } from './publishedContract'

// The map needs WebGL, so it is replaced by a stub exposing the interaction the
// planning flow depends on: placing a checkpoint by clicking.
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

const picture = readExample('operational-picture')
const missionList = readExample('patrol-mission-list')

interface Call {
  url: string
  method: string
  body: unknown
  headers: Record<string, string>
}

let calls: Call[] = []

interface Route {
  /** Matched against the request path. */
  path: string
  method?: string
  status?: number
  payload: unknown
}

function stubFetch(routes: Route[]) {
  const fetchStub = vi.fn(async (url: string, init?: RequestInit) => {
    const method = init?.method ?? 'GET'
    const headers = (init?.headers ?? {}) as Record<string, string>
    calls.push({
      url,
      method,
      body: init?.body === undefined ? undefined : JSON.parse(String(init.body)),
      headers,
    })

    const route = routes.find(
      (candidate) => url.includes(candidate.path) && (candidate.method ?? 'GET') === method,
    )
    if (route === undefined) throw new Error(`unexpected request: ${method} ${url}`)

    const status = route.status ?? 200
    return {
      ok: status < 400,
      status,
      json: async () => route.payload,
    }
  })
  vi.stubGlobal('fetch', fetchStub)
  return fetchStub
}

function defaultRoutes(): Route[] {
  return [
    { path: '/operational-picture', payload: picture },
    { path: '/missions', payload: missionList },
  ]
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  )
  return render(<PatrolMissionPage />, { wrapper })
}

beforeEach(() => {
  calls = []
  stubFetch(defaultRoutes())
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('PatrolMissionPage against the API', () => {
  it('shows a loading state before the first responses', () => {
    renderPage()

    expect(screen.getByRole('status')).toHaveTextContent('순찰 임무를 불러오는 중입니다.')
  })

  it('lists the missions the API returned with their coverage', async () => {
    renderPage()

    const list = within(await screen.findByLabelText('임무 목록'))
    expect(list.getByText('하천변 정기 순찰 (오전)')).toBeInTheDocument()
    expect(list.getByText('진행 중')).toBeInTheDocument()
    expect(list.getByText('통과 2 · 미순찰 0 · 판단 불가 0 / 5')).toBeInTheDocument()
    // The aborted mission whose link was lost reports nothing as missed.
    expect(list.getByText('통과 0 · 미순찰 0 · 판단 불가 3 / 3')).toBeInTheDocument()
  })

  it('shows the judgement and its evidence on the selected mission', async () => {
    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: /하천변 정기 순찰 \(오전\)/ }))

    const detail = within(screen.getByLabelText('하천변 정기 순찰 (오전)'))
    expect(detail.getByText('통과 2 · 미순찰 0 · 판단 불가 0 / 5')).toBeInTheDocument()
    expect(detail.getAllByText('통과')).toHaveLength(2)
    expect(detail.getAllByText('미실행')).toHaveLength(3)
    expect(detail.getByText(/정책 v1/)).toBeInTheDocument()
  })

  it('offers only the transitions the server allows', async () => {
    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: /하천변 정기 순찰 \(야간\)/ }))

    const detail = within(screen.getByLabelText('하천변 정기 순찰 (야간)'))
    expect(detail.getByText(/종료된 임무입니다/)).toBeInTheDocument()
    expect(detail.queryByRole('button', { name: '중단' })).not.toBeInTheDocument()
  })

  it('sends the version the operator saw with a transition', async () => {
    const approved = { ...(missionList as { missions: Record<string, unknown>[] }).missions[1] }
    stubFetch([
      ...defaultRoutes(),
      { path: '/approve', method: 'POST', payload: { ...approved, status: 'READY', version: 1 } },
    ])
    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: /공사 구역 상황 순찰/ }))
    fireEvent.click(screen.getByRole('button', { name: '승인' }))

    await waitFor(() => {
      expect(calls.some((call) => call.url.includes('/approve'))).toBe(true)
    })
    const approve = calls.find((call) => call.url.includes('/approve'))
    expect(approve?.body).toEqual({ version: 0 })
  })

  it('explains a refusal by its contract code', async () => {
    stubFetch([
      ...defaultRoutes(),
      {
        path: '/approve',
        method: 'POST',
        status: 409,
        payload: {
          code: 'ASSIGNMENT_CONFLICT',
          error: 'unit is already committed to an overlapping window: SG-01',
          details: { callsigns: ['SG-01'] },
        },
      },
    ])
    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: /공사 구역 상황 순찰/ }))
    fireEvent.click(screen.getByRole('button', { name: '승인' }))

    // The operator reads the console's wording for the code, not the server's
    // English sentence.
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'SG-01가 겹치는 시간대에 이미 배정돼 있습니다.',
    )
  })

  it('refuses an abort without a reason through the server', async () => {
    stubFetch([
      ...defaultRoutes(),
      {
        path: '/abort',
        method: 'POST',
        status: 422,
        payload: { code: 'ABORT_REASON_REQUIRED', error: 'abort requires a reason' },
      },
    ])
    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: /하천변 정기 순찰 \(오전\)/ }))
    fireEvent.click(
      within(screen.getByLabelText('하천변 정기 순찰 (오전)')).getByRole('button', { name: '중단' }),
    )

    expect(await screen.findByRole('alert')).toHaveTextContent('중단 사유를 선택해야 합니다.')
    const abort = calls.find((call) => call.url.includes('/abort'))
    expect(abort?.body).toEqual({ version: 3 })
  })

  it('sends the chosen abort reason', async () => {
    const aborted = (missionList as { missions: Record<string, unknown>[] }).missions[3]
    stubFetch([...defaultRoutes(), { path: '/abort', method: 'POST', payload: aborted }])
    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: /하천변 정기 순찰 \(오전\)/ }))
    const detail = within(screen.getByLabelText('하천변 정기 순찰 (오전)'))
    fireEvent.change(detail.getByLabelText('중단 사유'), { target: { value: 'BATTERY' } })
    fireEvent.click(detail.getByRole('button', { name: '중단' }))

    await waitFor(() => {
      expect(calls.some((call) => call.url.includes('/abort'))).toBe(true)
    })
    expect(calls.find((call) => call.url.includes('/abort'))?.body).toEqual({
      version: 3,
      reason: 'BATTERY',
    })
  })

  it('creates a draft from the planner with an idempotency key', async () => {
    const draft = (missionList as { missions: Record<string, unknown>[] }).missions[1]
    stubFetch([
      ...defaultRoutes(),
      { path: '/missions', method: 'POST', status: 201, payload: draft },
    ])
    renderPage()

    fireEvent.click(await screen.findByRole('button', { name: '새 임무 계획' }))
    fireEvent.change(screen.getByLabelText('임무 이름'), { target: { value: '오후 순찰' } })
    fireEvent.click(screen.getByRole('button', { name: '지도 클릭' }))
    fireEvent.click(screen.getByRole('button', { name: '지도 클릭' }))
    expect(screen.getByTestId('map-checkpoint-count')).toHaveTextContent('2')

    fireEvent.click(screen.getByRole('button', { name: '초안 저장' }))

    await waitFor(() => {
      expect(calls.some((call) => call.method === 'POST' && call.url.endsWith('/missions'))).toBe(true)
    })
    const create = calls.find((call) => call.method === 'POST' && call.url.endsWith('/missions'))
    expect(create?.headers['Idempotency-Key']).toBeTruthy()
    expect(create?.body).toMatchObject({
      name: '오후 순찰',
      patrolAreaId: '20000000-0000-0000-0000-000000000001',
    })
    expect((create?.body as { checkpoints: unknown[] }).checkpoints).toHaveLength(2)
  })

  it('shows an error state when the API cannot be reached', async () => {
    stubFetch([{ path: '/operational-picture', status: 503, payload: { code: 'PROJECTION_UNAVAILABLE', error: 'down' } }])
    renderPage()

    expect(await screen.findByRole('alert')).toHaveTextContent('Mission API에 연결할 수 없습니다.')
  })
})
