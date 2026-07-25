import { createFileRoute } from '@tanstack/react-router'
import { LoggedOutPage } from '@/pages/LoggedOutPage'

export const Route = createFileRoute('/logged-out')({
  component: LoggedOutPage,
})
