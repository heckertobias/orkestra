import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Layout } from './components/layout/Layout'
import { DashboardPage } from './pages/DashboardPage'
import { ServersPage } from './pages/ServersPage'
import { ServerDetailPage } from './pages/ServerDetailPage'
import { StacksPage } from './pages/StacksPage'
import { StackDetailPage } from './pages/StackDetailPage'
import { PlaceholderPage } from './pages/PlaceholderPage'

const queryClient = new QueryClient({
  defaultOptions: { queries: { staleTime: 10_000 } },
})

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route element={<Layout />}>
            <Route index element={<DashboardPage />} />
            <Route path="servers" element={<ServersPage />} />
            <Route path="servers/:id" element={<ServerDetailPage />} />
            <Route path="stacks"      element={<StacksPage />} />
            <Route path="stacks/:id"  element={<StackDetailPage />} />
            <Route path="secrets"  element={<PlaceholderPage title="Secrets"  description="Secret management — Milestone 4" />} />
            <Route path="users"    element={<PlaceholderPage title="Users"    description="User & role management — Milestone 5" />} />
            <Route path="settings" element={<PlaceholderPage title="Settings" />} />
          </Route>
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  )
}
