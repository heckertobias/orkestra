import { createFileRoute } from '@tanstack/react-router'
import { StackEditorPage } from '@/pages/StackEditorPage'

export const Route = createFileRoute('/_authenticated/stacks/new')({
  component: StackEditorPage,
})
