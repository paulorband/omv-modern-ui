import { useState, type FormEvent } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { ApiError } from '../lib/api'
import { useAuth } from '../lib/auth'
import { Button, Card, Field, inputCls } from '../components/ui'

export function Login() {
  const { user, login } = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const from = (location.state as { from?: string } | null)?.from ?? '/'

  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  if (user) return <Navigate to={from} replace />

  async function submit(e: FormEvent) {
    e.preventDefault()
    setError(null)
    setBusy(true)
    try {
      await login(username.trim(), password)
      navigate(from, { replace: true })
    } catch (err) {
      if (err instanceof ApiError && err.status === 403) setError('Apenas administradores do OMV podem entrar.')
      else if (err instanceof ApiError && err.status === 401) setError('Usuário ou senha inválidos.')
      else if (err instanceof ApiError && err.status === 429) setError(err.message)
      else if (err instanceof ApiError && err.status === 502) setError('Não consegui falar com o OMV. Verifique se o serviço está no ar.')
      else setError('Não foi possível entrar. Tente novamente.')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <Card className="w-full max-w-sm p-6">
        <div className="mb-6 text-center">
          <div className="mb-2 text-5xl" aria-hidden>
            🏠
          </div>
          <h1 className="text-xl font-semibold">OMV Home</h1>
          <p className="text-sm text-muted">Entre com seu usuário administrador do OpenMediaVault.</p>
        </div>

        <form onSubmit={submit} className="space-y-4" noValidate>
          <Field label="Usuário">
            <input className={inputCls} value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="username" autoFocus required />
          </Field>
          <Field label="Senha">
            <input className={inputCls} type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="current-password" required />
          </Field>

          {error && (
            <p role="alert" className="rounded-xl border border-danger/40 bg-danger/10 p-2.5 text-sm text-danger">
              {error}
            </p>
          )}

          <Button type="submit" variant="primary" className="w-full" loading={busy} disabled={!username || !password}>
            Entrar
          </Button>
        </form>
      </Card>
    </div>
  )
}
