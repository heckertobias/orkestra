import { createContext, useContext, useEffect, useState, useCallback } from 'react'

export interface AuthUser {
  id: string
  username: string
  displayName: string
  roles: string[]
  hasPassword: boolean
}

interface AuthCtx {
  user: AuthUser | null
  loading: boolean
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
  refresh: () => Promise<void>
}

const Ctx = createContext<AuthCtx>({
  user: null,
  loading: true,
  login: async () => {},
  logout: async () => {},
  refresh: async () => {},
})

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(null)
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async () => {
    try {
      const res = await fetch('/orkestra.v1.AuthService/GetCurrentUser', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      })
      if (res.ok) {
        const data = await res.json()
        setUser({
          id:          String(data.id ?? ''),
          username:    String(data.username ?? ''),
          displayName: String(data.displayName ?? data.display_name ?? ''),
          roles:       Array.isArray(data.roles) ? data.roles.map(String) : [],
          hasPassword: Boolean(data.hasPassword ?? data.has_password ?? false),
        })
      } else {
        setUser(null)
      }
    } catch {
      setUser(null)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { refresh() }, [refresh])

  const login = useCallback(async (username: string, password: string) => {
    const res = await fetch('/orkestra.v1.AuthService/Login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    })
    if (!res.ok) {
      const text = await res.text().catch(() => `HTTP ${res.status}`)
      throw new Error(text)
    }
    await refresh()
  }, [refresh])

  const logout = useCallback(async () => {
    await fetch('/orkestra.v1.AuthService/Logout', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({}),
    })
    setUser(null)
  }, [])

  return <Ctx.Provider value={{ user, loading, login, logout, refresh }}>{children}</Ctx.Provider>
}

export function useAuth() {
  return useContext(Ctx)
}

export function isAdmin(user: AuthUser | null) {
  return user?.roles.includes('admin') ?? false
}
