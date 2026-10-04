import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { get, onUnauthorized, post } from './api'
import type { User } from './types'

type AuthState = {
  /** undefined while the first /auth/me check is running. */
  user: User | null | undefined
  login: (username: string, password: string) => Promise<void>
  logout: () => Promise<void>
}

const Ctx = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient()
  const [user, setUser] = useState<User | null | undefined>(undefined)

  useEffect(() => {
    let alive = true
    get<{ user: User }>('/auth/me')
      .then((r) => alive && setUser(r.user))
      .catch(() => alive && setUser(null))
    return () => {
      alive = false
    }
  }, [])

  // Any 401 from the API (expired session) sends the user back to the login page (R01.8).
  useEffect(
    () =>
      onUnauthorized(() => {
        setUser(null)
        qc.clear()
      }),
    [qc],
  )

  const login = useCallback(async (username: string, password: string) => {
    const r = await post<{ user: User }>('/auth/login', { username, password })
    setUser(r.user)
  }, [])

  const logout = useCallback(async () => {
    try {
      await post('/auth/logout')
    } finally {
      setUser(null)
      qc.clear()
    }
  }, [qc])

  const value = useMemo(() => ({ user, login, logout }), [user, login, logout])
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}

export function useAuth(): AuthState {
  const v = useContext(Ctx)
  if (!v) throw new Error('useAuth must be used inside <AuthProvider>')
  return v
}
