import { createFileRoute, redirect } from '@tanstack/react-router'
import { Layout } from '@/components/layout/Layout'
import { fetchCurrentUser, CURRENT_USER_KEY } from '@/lib/auth'

// Pathless layout route guarding every authenticated page. The check runs before render
// (no login flash): ensureQueryData awaits the current user, and a null result redirects
// to /login. The result is cached, so AuthProvider's useQuery reuses it without refetching.
export const Route = createFileRoute('/_authenticated')({
  beforeLoad: async ({ context }) => {
    const user = await context.queryClient.ensureQueryData({
      queryKey: CURRENT_USER_KEY,
      queryFn: fetchCurrentUser,
    })
    if (!user) throw redirect({ to: '/login' })
  },
  component: Layout,
})
