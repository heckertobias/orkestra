import { createFileRoute } from '@tanstack/react-router'
import { StacksPage } from '@/pages/StacksPage'

export const Route = createFileRoute('/_authenticated/stacks/')({
  component: StacksPage,
})
