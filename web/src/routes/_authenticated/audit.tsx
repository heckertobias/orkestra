import { createFileRoute } from '@tanstack/react-router'
import { AuditLogPage } from '@/pages/AuditLogPage'

export const Route = createFileRoute('/_authenticated/audit')({
  component: AuditLogPage,
})
