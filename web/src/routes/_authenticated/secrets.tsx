import { createFileRoute } from '@tanstack/react-router'
import { SecretsPage } from '@/pages/SecretsPage'

export const Route = createFileRoute('/_authenticated/secrets')({
  component: SecretsPage,
})
