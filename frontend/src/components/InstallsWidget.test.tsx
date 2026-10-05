import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { InstallsWidget, visibleInstalls, DONE_VISIBLE_MS } from './InstallsWidget'
import { FakeEventSource, mockApi, renderApp } from '../test-utils'
import type { InstallJob } from '../lib/types'

const T0 = '2026-10-05T10:00:00Z'
const job = (over: Partial<InstallJob> & Pick<InstallJob, 'id'>): InstallJob => ({ title: over.id, state: 'running', startedAt: T0, ...over })

beforeEach(() => {
  FakeEventSource.reset()
  vi.stubGlobal('EventSource', FakeEventSource)
})
afterEach(() => vi.unstubAllGlobals())

describe('visibleInstalls (R10.10)', () => {
  const now = Date.parse(T0) + 10 * 60_000
  it('keeps running and failed installs, drops old successes', () => {
    const jobs = [
      job({ id: 'a' }),
      job({ id: 'b', state: 'error', finishedAt: T0 }),
      job({ id: 'old', state: 'done', finishedAt: T0 }),
      job({ id: 'new', state: 'done', finishedAt: new Date(now - DONE_VISIBLE_MS + 5000).toISOString() }),
    ]
    expect(visibleInstalls(jobs, now).map((j) => j.id)).toEqual(['a', 'b', 'new'])
  })
})

describe('InstallsWidget (SPEC-10)', () => {
  it('renders nothing when there are no installs (R10.7)', async () => {
    const calls = mockApi([{ path: '/store/installs', body: [] }])
    renderApp(<InstallsWidget />)
    await waitFor(() => expect(calls.some((c) => c.path === '/store/installs')).toBe(true))
    await new Promise((r) => setTimeout(r, 20))
    expect(screen.queryByRole('region', { name: 'Instalações' })).not.toBeInTheDocument()
  })

  const MIXED: InstallJob[] = [
    job({ id: 'jellyfin', title: 'Jellyfin', message: 'Pulling jellyfin/jellyfin' }),
    job({ id: 'pihole', title: 'Pi-hole', message: 'Creating container' }),
    job({ id: 'nextcloud', title: 'Nextcloud', state: 'error', finishedAt: T0, message: 'pull access denied' }),
  ]

  it('summarises several concurrent installs and expands to a list (A10.5)', async () => {
    mockApi([{ path: '/store/installs', body: MIXED }])
    renderApp(<InstallsWidget />)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Instalações: 2 instalando · 1 falhou' }))
    const panel = screen.getByRole('region', { name: 'Instalações' })
    expect(within(panel).getByText('Jellyfin')).toBeInTheDocument()
    expect(within(panel).getByText('Pulling jellyfin/jellyfin')).toBeInTheDocument()
    expect(within(panel).getByText('Pi-hole')).toBeInTheDocument()
    expect(within(panel).getByText('pull access denied')).toBeInTheDocument()
    // only finished installs can be dismissed
    expect(within(panel).queryByRole('button', { name: 'Dispensar Jellyfin' })).not.toBeInTheDocument()
    expect(within(panel).getByRole('button', { name: 'Dispensar Nextcloud' })).toBeInTheDocument()
    await user.click(within(panel).getByRole('button', { name: 'Recolher painel de instalações' }))
    expect(screen.getByRole('button', { name: /Instalações:/ })).toBeInTheDocument()
  })

  it('opens the full log of a failed install, with the error highlighted (R10.9)', async () => {
    mockApi([{ path: '/store/installs', body: MIXED }])
    renderApp(<InstallsWidget />)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: /Instalações:/ }))
    await user.click(screen.getByRole('button', { name: 'Ver log de Nextcloud' }))

    const dialog = await screen.findByRole('dialog', { name: 'Instalação de Nextcloud' })
    await waitFor(() => expect(FakeEventSource.last('/store/installs/nextcloud/events')).toBeDefined())
    const es = FakeEventSource.last('/store/installs/nextcloud/events')!
    act(() => {
      es.emit('starting', { type: 'starting', message: 'Iniciando instalação de nextcloud' })
      es.emit('pull', { type: 'pull', message: 'Pulling nextcloud' })
      es.emit('log', { type: 'log', message: 'Error response from daemon: unauthorized' })
      es.emit('error', { type: 'error', message: 'pull access denied' })
    })
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('Não foi possível instalar: pull access denied')
    expect(within(dialog).getByText('Error response from daemon: unauthorized')).toBeInTheDocument()
    expect(within(dialog).getByText('Iniciando instalação de nextcloud')).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: /Copiar log/ })).toBeInTheDocument()

    await user.click(within(dialog).getAllByRole('button', { name: 'Fechar' }).at(-1)!)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('dismisses a finished install through the API (A10.6)', async () => {
    const calls = mockApi([
      { path: '/store/installs', body: MIXED },
      { method: 'DELETE', path: '/store/installs/nextcloud', status: 204 },
    ])
    renderApp(<InstallsWidget />)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: /Instalações:/ }))
    await user.click(screen.getByRole('button', { name: 'Dispensar Nextcloud' }))
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.path === '/store/installs/nextcloud')).toBe(true))
  })

  it('links finished installs to the app', async () => {
    mockApi([{ path: '/store/installs', body: [job({ id: 'jellyfin', title: 'Jellyfin', state: 'done', finishedAt: new Date().toISOString(), message: 'Instalação concluída' })] }])
    renderApp(<InstallsWidget />)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Instalações: Instalação concluída' }))
    expect(screen.getByRole('link', { name: 'Abrir' })).toHaveAttribute('href', '/?highlight=jellyfin')
  })

  it('announces an install that finishes while the user is elsewhere (R10.11, A10.6)', async () => {
    const routes = [{ path: '/store/installs', body: [job({ id: 'jellyfin', title: 'Jellyfin' })] as InstallJob[] }]
    mockApi(routes)
    renderApp(<InstallsWidget />)
    await screen.findByRole('button', { name: 'Instalações: 1 instalando' })

    routes[0].body = [job({ id: 'jellyfin', title: 'Jellyfin', state: 'done', finishedAt: new Date().toISOString() })]
    act(() => {
      window.dispatchEvent(new Event('visibilitychange')) // react-query refetches on focus
    })
    expect(await screen.findByText('Jellyfin foi instalado.')).toBeInTheDocument()
  })

  it('announces a failure and opens the panel (R10.11)', async () => {
    const routes = [{ path: '/store/installs', body: [job({ id: 'nextcloud', title: 'Nextcloud' })] as InstallJob[] }]
    mockApi(routes)
    renderApp(<InstallsWidget />)
    await screen.findByRole('button', { name: 'Instalações: 1 instalando' })

    routes[0].body = [job({ id: 'nextcloud', title: 'Nextcloud', state: 'error', finishedAt: new Date().toISOString(), message: 'boom' })]
    act(() => {
      window.dispatchEvent(new Event('visibilitychange'))
    })
    expect(await screen.findByText(/Falha ao instalar Nextcloud/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Ver log de Nextcloud' })).toBeInTheDocument() // panel opened
  })

  it('does not announce installs it first sees already finished', async () => {
    mockApi([{ path: '/store/installs', body: [job({ id: 'x', title: 'X', state: 'error', finishedAt: T0 })] }])
    renderApp(<InstallsWidget />)
    await screen.findByRole('button', { name: 'Instalações: 1 falhou' })
    expect(screen.queryByText(/Falha ao instalar/)).not.toBeInTheDocument()
  })
})
