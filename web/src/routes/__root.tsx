import { createRootRouteWithContext, Outlet } from '@tanstack/react-router'
import type { QueryClient } from '@tanstack/react-query'

// Providers (QueryClient, Auth, Toast) live around <RouterProvider> in App.tsx, so the
// root route only renders the matched child route.
export const Route = createRootRouteWithContext<{ queryClient: QueryClient }>()({
  component: () => <Outlet />,
})
