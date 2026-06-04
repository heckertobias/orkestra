import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Layout } from './components/layout/Layout'
import { DashboardPage } from './pages/DashboardPage'
import { ServersPage } from './pages/ServersPage'
import { ServerDetailPage } from './pages/ServerDetailPage'
import { StacksPage } from './pages/StacksPage'
import { StackDetailPage } from './pages/StackDetailPage'
import { SecretsPage } from './pages/SecretsPage'
import { LoginPage } from './pages/LoginPage'
import { UsersPage } from './pages/UsersPage'
import { AuditLogPage } from './pages/AuditLogPage'
import { PlaceholderPage } from './pages/PlaceholderPage'
import { AuthProvider, useAuth } from './lib/auth'

const queryClient = new QueryClient({
  defaultOptions: { queries: { staleTime: 10_000 } },
})

function AuthGuard({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuth()
  if (loading) return null
  if (!user) return <Navigate to="/login" replace />
  return <>{children}</>
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <AuthProvider>
          <Routes>
            <Route path="login" element={<LoginPage />} />
            <Route element={<AuthGuard><Layout /></AuthGuard>}>
              <Route index element={<DashboardPage />} />
              <Route path="servers" element={<ServersPage />} />
              <Route path="servers/:id" element={<ServerDetailPage />} />
              <Route path="stacks"      element={<StacksPage />} />
              <Route path="stacks/:id"  element={<StackDetailPage />} />
              <Route path="secrets"  element={<SecretsPage />} />
              <Route path="users"    element={<UsersPage />} />
              <Route path="audit"    element={<AuditLogPage />} />
              <Route path="settings" element={<PlaceholderPage title="Settings" />} />
            </Route>
          </Routes>
        </AuthProvider>
      </BrowserRouter>
    </QueryClientProvider>
  )
}
