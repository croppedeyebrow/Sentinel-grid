import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { OperationalPicturePage } from '../pages/operational-picture/OperationalPicturePage'
import { PatrolMissionPage } from '../pages/patrol-mission/PatrolMissionPage'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { refetchInterval: 10_000, retry: 1 },
  },
})

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<OperationalPicturePage />} />
          <Route path="/missions" element={<PatrolMissionPage />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  )
}
