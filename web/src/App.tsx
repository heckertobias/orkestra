import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider, createRouter } from '@tanstack/react-router'
import { routeTree } from './routeTree.gen'
import { AuthProvider } from './lib/AuthProvider'
import { ToastProvider } from './components/ui/toast'

const queryClient = new QueryClient({
  defaultOptions: { queries: { staleTime: 10_000 } },
})

const router = createRouter({
  routeTree,
  context: { queryClient },
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

// AuthProvider must stay outside the router so routed pages can call useAuth() for RBAC
// and login/logout actions; the router's beforeLoad guard shares the same react-query cache.
export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <ToastProvider>
          <RouterProvider router={router} />
        </ToastProvider>
      </AuthProvider>
    </QueryClientProvider>
  )
}
