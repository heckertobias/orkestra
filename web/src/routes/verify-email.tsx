import { createFileRoute } from '@tanstack/react-router'
import { VerifyEmailPage } from '@/pages/VerifyEmailPage'

export interface TokenSearch {
  token?: string
}

export const Route = createFileRoute('/verify-email')({
  validateSearch: (search: Record<string, unknown>): TokenSearch => ({
    token: typeof search.token === 'string' ? search.token : undefined,
  }),
  component: VerifyEmailPage,
})
