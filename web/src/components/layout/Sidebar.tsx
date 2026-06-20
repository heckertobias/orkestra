import { NavLink, useNavigate } from 'react-router-dom'
import { Server, Layers, KeyRound, Users, LayoutDashboard, Settings, ClipboardList, LogOut } from 'lucide-react'
import { cn } from '@/lib/cn'
import { useAuth, isAdmin, canManageSecrets } from '@/lib/auth'

export function Sidebar() {
  const { user, logout } = useAuth()
  const navigate = useNavigate()
  const admin = isAdmin(user)
  const secretsMgr = canManageSecrets(user)

  async function handleLogout() {
    await logout()
    navigate('/login', { replace: true })
  }

  return (
    <aside
      className="flex flex-col w-52 shrink-0 h-full border-r"
      style={{ backgroundColor: 'var(--surface)', borderColor: 'var(--border)' }}
    >
      {/* Logo */}
      <div className="flex items-center gap-3 px-4 py-4 border-b" style={{ borderColor: 'var(--border)' }}>
        <div
          className="w-7 h-7 rounded flex items-center justify-center text-xs font-bold"
          style={{ backgroundColor: 'var(--accent)', color: '#0d1117' }}
        >
          O
        </div>
        <span className="font-semibold text-sm" style={{ color: 'var(--text)' }}>
          ork<span style={{ color: 'var(--accent)' }}>estra</span>
        </span>
      </div>

      {/* Navigation */}
      <nav className="flex-1 px-2 py-3 space-y-0.5">
        {[
          { to: '/',         label: 'Dashboard', icon: LayoutDashboard, show: true },
          { to: '/servers',  label: 'Servers',   icon: Server,          show: true },
          { to: '/stacks',   label: 'Stacks',    icon: Layers,          show: true },
          { to: '/secrets',  label: 'Secrets',   icon: KeyRound,        show: secretsMgr },
          { to: '/users',    label: 'Users',     icon: Users,           show: admin },
          { to: '/audit',    label: 'Audit Log', icon: ClipboardList,   show: admin },
          { to: '/settings', label: 'Settings',  icon: Settings,        show: admin },
        ].filter(item => item.show).map(({ to, label, icon: Icon }) => (
          <NavLink
            key={to}
            to={to}
            end={to === '/'}
            className={({ isActive }) => cn(
              'flex items-center gap-3 px-3 py-2 rounded text-sm transition-colors',
              isActive
                ? 'text-[var(--accent)] bg-[rgba(126,226,42,0.08)]'
                : 'text-[var(--text-muted)] hover:text-[var(--text)] hover:bg-[var(--surface-2)]',
            )}
          >
            <Icon size={16} />
            {label}
          </NavLink>
        ))}
      </nav>

      {/* User info + logout */}
      {user && (
        <div className="px-3 py-3 border-t" style={{ borderColor: 'var(--border)' }}>
          <div className="flex items-center justify-between gap-2">
            <div className="min-w-0">
              <p className="text-xs font-medium truncate" style={{ color: 'var(--text)' }}>
                {user.displayName || user.username}
              </p>
              <p className="text-xs truncate" style={{ color: 'var(--text-muted)' }}>
                {user.roles.join(', ') || 'no role'}
              </p>
            </div>
            <button
              onClick={handleLogout}
              className="p-1.5 rounded hover:bg-[var(--surface-2)] shrink-0"
              style={{ color: 'var(--text-muted)' }}
              title="Sign out"
            >
              <LogOut size={14} />
            </button>
          </div>
        </div>
      )}
    </aside>
  )
}
