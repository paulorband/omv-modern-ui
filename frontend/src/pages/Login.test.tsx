import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes } from 'react-router-dom'
import { App } from '../App'
import { mockApi, renderApp } from '../test-utils'
import { Login } from './Login'

afterEach(() => vi.unstubAllGlobals())

async function fillAndSubmit(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText('Usuário'), 'admin')
  await user.type(screen.getByLabelText('Senha'), 'wrong')
  await user.click(screen.getByRole('button', { name: 'Entrar' }))
}

describe('Login', () => {
  it('shows a clear error for invalid credentials (A01.8)', async () => {
    mockApi([
      { path: '/auth/me', status: 401, body: { error: { code: 'unauthorized', message: 'x' } } },
      { method: 'POST', path: '/auth/login', status: 401, body: { error: { code: 'unauthorized', message: 'usuário ou senha inválidos' } } },
    ])
    renderApp(<Login />, { route: '/login' })
    await fillAndSubmit(userEvent.setup())
    expect(await screen.findByRole('alert')).toHaveTextContent('Usuário ou senha inválidos.')
  })

  it('explains the non-admin rejection', async () => {
    mockApi([
      { path: '/auth/me', status: 401 },
      { method: 'POST', path: '/auth/login', status: 403, body: { error: { code: 'forbidden', message: 'x' } } },
    ])
    renderApp(<Login />, { route: '/login' })
    await fillAndSubmit(userEvent.setup())
    expect(await screen.findByRole('alert')).toHaveTextContent('Apenas administradores')
  })

  it('sends credentials with the CSRF header and leaves the login page', async () => {
    const calls = mockApi([
      { path: '/auth/me', status: 401 },
      { method: 'POST', path: '/auth/login', body: { user: { username: 'admin', role: 'admin' } } },
    ])
    renderApp(
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/" element={<p>dentro</p>} />
      </Routes>,
      { route: '/login' },
    )
    await fillAndSubmit(userEvent.setup())
    expect(await screen.findByText('dentro')).toBeInTheDocument()
    const login = calls.find((c) => c.method === 'POST')!
    expect(login.body).toEqual({ username: 'admin', password: 'wrong' })
    const init = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls.find((c) => c[1]?.method === 'POST')![1]
    expect(init.headers['X-Requested-With']).toBe('omv-home')
  })

  it('disables submit until both fields are filled', async () => {
    mockApi([{ path: '/auth/me', status: 401 }])
    renderApp(<Login />, { route: '/login' })
    expect(screen.getByRole('button', { name: 'Entrar' })).toBeDisabled()
  })
})

describe('route guard (A01.8)', () => {
  it('redirects anonymous visitors to the login page', async () => {
    mockApi([{ path: '/auth/me', status: 401 }])
    renderApp(<App />, { route: '/files' })
    await waitFor(() => expect(screen.getByRole('heading', { name: 'OMV Home' })).toBeInTheDocument())
    expect(screen.getByLabelText('Usuário')).toBeInTheDocument()
  })

  it('shows protected pages to a logged-in user', async () => {
    mockApi([{ path: '/auth/me', body: { user: { username: 'admin', role: 'admin' } } }])
    renderApp(<App />, { route: '/files' })
    expect(await screen.findByText('Em breve')).toBeInTheDocument()
  })
})
