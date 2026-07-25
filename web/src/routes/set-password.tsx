import { createFileRoute } from '@tanstack/react-router'
import { SetPasswordPage } from '@/pages/SetPasswordPage'

export interface TokenSearch {
  token?: string
}

export const Route = createFileRoute('/set-password')({
  validateSearch: (search: Record<string, unknown>): TokenSearch => ({
    token: typeof search.token === 'string' ? search.token : undefined,
  }),
  component: SetPasswordPage,
})
