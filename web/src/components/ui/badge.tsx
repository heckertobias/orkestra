import { cn } from '@/lib/cn'

type BadgeVariant = 'online' | 'offline' | 'warn' | 'error' | 'default'

const styles: Record<BadgeVariant, string> = {
  online:  'bg-[#1a3a22] text-[#3fb950] border border-[#2d5a3a]',
  offline: 'bg-[#1c1f24] text-[#8b949e] border border-[#30363d]',
  warn:    'bg-[#2f2008] text-[#d29922] border border-[#4a3000]',
  error:   'bg-[#2d1115] text-[#f85149] border border-[#4a1a1f]',
  default: 'bg-[var(--surface-2)] text-[var(--text-muted)] border border-[var(--border)]',
}

interface BadgeProps {
  variant?: BadgeVariant
  children: React.ReactNode
  className?: string
}

export function Badge({ variant = 'default', children, className }: BadgeProps) {
  return (
    <span className={cn(
      'inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-medium',
      styles[variant],
      className,
    )}>
      {children}
    </span>
  )
}

export function StatusDot({ online }: { online: boolean }) {
  return (
    <span
      className={cn(
        'inline-block w-2 h-2 rounded-full',
        online ? 'bg-[#3fb950]' : 'bg-[#8b949e]',
      )}
    />
  )
}
