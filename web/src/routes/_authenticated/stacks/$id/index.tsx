import { createFileRoute } from '@tanstack/react-router'
import { StackDetailPage } from '@/pages/StackDetailPage'

export const Route = createFileRoute('/_authenticated/stacks/$id/')({
  component: StackDetailPage,
})
