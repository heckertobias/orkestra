import { useCallback } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Ctx, fetchCurrentUser } from './auth'

const CURRENT_USER_KEY = ['currentUser'] as const

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const qc = useQueryClient()

  const { data: user = null, isPending: loading } = useQuery({
    queryKey: CURRENT_USER_KEY,
    queryFn: fetchCurrentUser,
    staleTime: Infinity, // only refetched explicitly via login/logout/refresh
  })

  const refresh = useCallback(async () => {
    await qc.invalidateQueries({ queryKey: CURRENT_USER_KEY })
  }, [qc])

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
    await qc.invalidateQueries({ queryKey: CURRENT_USER_KEY })
  }, [qc])

  const logout = useCallback(async (): Promise<string | null> => {
    let postLogoutUrl: string | null = null
    try {
      const res = await fetch('/orkestra.v1.AuthService/Logout', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({}),
      })
      if (res.ok) {
        const d = await res.json().catch(() => ({}))
        postLogoutUrl = d.postLogoutUrl || d.post_logout_url || null
      }
    } finally {
      qc.setQueryData(CURRENT_USER_KEY, null)
    }
    return postLogoutUrl
  }, [qc])

  return <Ctx.Provider value={{ user, loading, login, logout, refresh }}>{children}</Ctx.Provider>
}
