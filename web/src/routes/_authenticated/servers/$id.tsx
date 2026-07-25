import { createFileRoute } from '@tanstack/react-router'
import { ServerDetailPage } from '@/pages/ServerDetailPage'

export const Route = createFileRoute('/_authenticated/servers/$id')({
  component: ServerDetailPage,
})
