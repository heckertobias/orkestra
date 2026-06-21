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
import { ForgotPasswordPage } from './pages/ForgotPasswordPage'
import { SetPasswordPage } from './pages/SetPasswordPage'
import { UsersPage } from './pages/UsersPage'
import { AuditLogPage } from './pages/AuditLogPage'
import { SettingsPage } from './pages/SettingsPage'
import { ProfilePage } from './pages/ProfilePage'
import { AuthProvider, useAuth } from './lib/auth'
import { ToastProvider } from './components/ui/toast'

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
          <ToastProvider>
            <Routes>
              {/* Public routes — no auth required */}
              <Route path="login"            element={<LoginPage />} />
              <Route path="forgot-password"  element={<ForgotPasswordPage />} />
              <Route path="set-password"     element={<SetPasswordPage />} />
              {/* Authenticated routes */}
              <Route element={<AuthGuard><Layout /></AuthGuard>}>
                <Route index element={<DashboardPage />} />
                <Route path="servers" element={<ServersPage />} />
                <Route path="servers/:id" element={<ServerDetailPage />} />
                <Route path="stacks"      element={<StacksPage />} />
                <Route path="stacks/:id"  element={<StackDetailPage />} />
                <Route path="secrets"  element={<SecretsPage />} />
                <Route path="users"    element={<UsersPage />} />
                <Route path="audit"    element={<AuditLogPage />} />
                <Route path="settings" element={<SettingsPage />} />
                <Route path="profile"  element={<ProfilePage />} />
              </Route>
            </Routes>
          </ToastProvider>
        </AuthProvider>
      </BrowserRouter>
    </QueryClientProvider>
  )
}
