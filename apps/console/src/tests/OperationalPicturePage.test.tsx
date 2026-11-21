import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen, within } from '@testing-library/react'
import type { ReactNode } from 'react'
import { MemoryRouter } from 'react-router-dom'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { OperationalPicturePage } from '../pages/operational-picture/OperationalPicturePage'
import { readPublishedExample } from './publishedContract'

// MapLibre needs a WebGL context that jsdom does not provide, and the map is
// covered separately by its own props. The page test is about the states an
// operator sees.
vi.mock('../widgets/patrol-map/PatrolMap', () => ({
  PatrolMap: ({ units }: { units: readonly { id: string }[] }) => (
    <div data-testid="patrol-map">{units.length}</div>
  ),
}))

const example = readPublishedExample()

function respondWith(payload: unknown, ok = true) {
  return vi.fn().mockResolvedValue({
    ok,
    status: ok ? 200 : 503,
    json: async () => payload,
  })
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>
      <MemoryRouter>{children}</MemoryRouter>
    </QueryClientProvider>
  )
  return render(<OperationalPicturePage />, { wrapper })
}

beforeEach(() => {
  vi.stubGlobal('fetch', respondWith(example))
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('OperationalPicturePage', () => {
  it('shows a loading state before the first response', () => {
    renderPage()

    expect(screen.getByRole('status')).toHaveTextContent('운영상황도를 불러오는 중입니다.')
  })

  it('renders both axes for every unit', async () => {
    renderPage()

    expect(await screen.findByText('Operational Picture')).toBeInTheDocument()

    const badges = screen.getAllByTestId('status-badge').map((badge) => badge.textContent)
    expect(badges).toEqual(['정상', '판단 불가', '판단 불가', '판단 불가', '주의', '위험'])

    const chips = new Set(
      screen.getAllByTestId('freshness-chip').map((chip) => chip.textContent?.slice(0, 2)),
    )
    expect(chips).toEqual(new Set(['최신', '지연', '누락']))
  })

  it('explains an unknown condition with the observations behind it', async () => {
    renderPage()

    const stale = within(await screen.findByLabelText('SG-02'))
    expect(
      stale.getByText('근거 부족: 위치 관측 지연, 배터리 관측 지연, 링크 관측 지연'),
    ).toBeInTheDocument()
    // The last known position stays on screen, marked as not current.
    expect(stale.getByText('37.5685, 126.9820')).toBeInTheDocument()
  })

  it('explains a warning with the site threshold', async () => {
    renderPage()

    const warning = within(await screen.findByLabelText('SG-05'))
    expect(warning.getByText('배터리 35% (주의 기준 40% 이하)')).toBeInTheDocument()
  })

  it('counts untrusted positions on the data quality axis, not the condition axis', async () => {
    renderPage()

    // Three units are UNKNOWN, but only two of them have a position that
    // cannot be trusted: SG-03 reports a fresh position with no battery.
    expect(await screen.findByText('위치 미신뢰 2 / 6')).toBeInTheDocument()
    expect(screen.getAllByTestId('status-badge').filter((b) => b.textContent === '판단 불가')).toHaveLength(3)
  })

  it('passes the site context and units to the map', async () => {
    renderPage()

    expect(await screen.findByTestId('patrol-map')).toHaveTextContent('6')
  })

  it('shows an empty state for a site with no units', async () => {
    vi.stubGlobal('fetch', respondWith({ ...example, units: [] }))
    renderPage()

    expect(await screen.findByText('표시할 순찰체가 없습니다.')).toBeInTheDocument()
    expect(screen.queryAllByTestId('status-badge')).toHaveLength(0)
  })

  it('shows an error state when the API is unavailable', async () => {
    vi.stubGlobal('fetch', respondWith({}, false))
    renderPage()

    const alert = await screen.findByRole('alert')
    expect(alert).toHaveTextContent('Mission API에 연결할 수 없습니다.')
    expect(within(alert).getByRole('button', { name: '다시 시도' })).toBeInTheDocument()
  })

  it('shows an error state when the response breaks the contract', async () => {
    vi.stubGlobal('fetch', respondWith({ generatedAt: '2026-11-12T09:00:00Z', units: [] }))
    renderPage()

    expect(await screen.findByRole('alert')).toBeInTheDocument()
  })
})
