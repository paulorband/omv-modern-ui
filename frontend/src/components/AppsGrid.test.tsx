import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { AppsGrid, filterApps } from './AppsGrid'
import { AppIcon } from './AppIcon'
import { FakeEventSource, mockApi, renderApp } from '../test-utils'
import type { App } from '../lib/types'

const app = (over: Partial<App>): App => ({
  id: 'x', name: 'X', icon: '', category: 'Other', state: 'running', port: 8080, scheme: 'http', path: '', managed: true, containers: ['x'], ...over,
})

const APPS: App[] = [
  app({ id: 'jellyfin', name: 'Jellyfin', icon: 'emoji:🎬', category: 'Media', port: 8096 }),
  app({ id: 'pihole', name: 'Pi-hole', category: 'Network', port: 8081 }),
  app({ id: 'syncthing', name: 'Syncthing', category: 'Files', state: 'stopped', port: 8384 }),
]

beforeEach(() => {
  FakeEventSource.reset()
  vi.stubGlobal('EventSource', FakeEventSource)
})
afterEach(() => vi.unstubAllGlobals())

describe('filterApps (A04.5)', () => {
  it('filters by name and category', () => {
    expect(filterApps(APPS, 'jelly', 'Todos').map((a) => a.id)).toEqual(['jellyfin'])
    expect(filterApps(APPS, '', 'Network').map((a) => a.id)).toEqual(['pihole'])
    expect(filterApps(APPS, 'PI-', 'Todos').map((a) => a.id)).toEqual(['pihole'])
    expect(filterApps(APPS, 'zzz', 'Todos')).toEqual([])
    expect(filterApps(APPS, '', 'Todos')).toHaveLength(3)
  })
})

describe('AppIcon (R04.10)', () => {
  it('renders emoji icons', () => {
    renderApp(<AppIcon icon="emoji:🎬" name="Jellyfin" />)
    expect(screen.getByText('🎬')).toBeInTheDocument()
  })
  it('falls back to the initial when there is no icon', () => {
    renderApp(<AppIcon name="pihole" />)
    expect(screen.getByTestId('icon-fallback')).toHaveTextContent('P')
  })
  it('falls back to the initial when the image fails to load', () => {
    const { container } = renderApp(<AppIcon icon="https://example.invalid/x.png" name="Vault" />)
    fireEvent.error(container.querySelector('img')!)
    expect(screen.getByTestId('icon-fallback')).toHaveTextContent('V')
  })
})

describe('AppsGrid', () => {
  it('renders cards, opens running apps and offers to start stopped ones', async () => {
    mockApi([{ path: '/apps', body: APPS }])
    renderApp(<AppsGrid />)
    const open = await screen.findByRole('link', { name: 'Abrir Jellyfin' })
    expect(open).toHaveAttribute('href', `http://${window.location.hostname}:8096`)
    expect(open).toHaveAttribute('target', '_blank')
    expect(screen.getByRole('button', { name: 'Iniciar Syncthing' })).toBeInTheDocument()
  })

  it('filters by search text', async () => {
    mockApi([{ path: '/apps', body: APPS }])
    renderApp(<AppsGrid />)
    await screen.findByText('Jellyfin')
    await userEvent.setup().type(screen.getByLabelText('Buscar apps'), 'pi')
    expect(screen.queryByText('Jellyfin')).not.toBeInTheDocument()
    expect(screen.getByText('Pi-hole')).toBeInTheDocument()
  })

  it('filters by category tabs', async () => {
    mockApi([{ path: '/apps', body: APPS }])
    renderApp(<AppsGrid />)
    await screen.findByText('Jellyfin')
    await userEvent.setup().click(screen.getByRole('tab', { name: 'Media' }))
    expect(screen.getByText('Jellyfin')).toBeInTheDocument()
    expect(screen.queryByText('Pi-hole')).not.toBeInTheDocument()
  })

  it('starts a stopped app when its card is clicked', async () => {
    const calls = mockApi([
      { path: '/apps', body: APPS },
      { method: 'POST', path: '/apps/syncthing/start', status: 204 },
    ])
    renderApp(<AppsGrid />)
    await userEvent.setup().click(await screen.findByRole('button', { name: 'Iniciar Syncthing' }))
    await waitFor(() => expect(calls.some((c) => c.method === 'POST' && c.path === '/apps/syncthing/start')).toBe(true))
    expect(await screen.findByText('Syncthing iniciado.')).toBeInTheDocument()
  })

  it('stops an app from the card menu', async () => {
    const calls = mockApi([
      { path: '/apps', body: APPS },
      { method: 'POST', path: '/apps/pihole/stop', status: 204 },
    ])
    renderApp(<AppsGrid />)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Ações de Pi-hole' }))
    await user.click(screen.getByRole('menuitem', { name: 'Parar' }))
    await waitFor(() => expect(calls.some((c) => c.path === '/apps/pihole/stop')).toBe(true))
  })

  it('surfaces action errors as a toast', async () => {
    mockApi([
      { path: '/apps', body: APPS },
      { method: 'POST', path: '/apps/pihole/restart', status: 502, body: { error: { code: 'upstream_error', message: 'Docker indisponível' } } },
    ])
    renderApp(<AppsGrid />)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Ações de Pi-hole' }))
    await user.click(screen.getByRole('menuitem', { name: 'Reiniciar' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Pi-hole: Docker indisponível')
  })

  it('requires typing the app name before purging data (R04.5)', async () => {
    const calls = mockApi([
      { path: '/apps', body: APPS },
      { method: 'DELETE', path: '/apps/pihole?purgeData=true', status: 204 },
    ])
    renderApp(<AppsGrid />)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Ações de Pi-hole' }))
    await user.click(screen.getByRole('menuitem', { name: 'Remover' }))

    const dialog = screen.getByRole('dialog', { name: 'Remover Pi-hole' })
    const confirmBtn = within(dialog).getByRole('button', { name: 'Remover' })
    expect(confirmBtn).toBeEnabled() // plain removal keeps data

    await user.click(within(dialog).getByRole('checkbox'))
    expect(confirmBtn).toBeDisabled()
    await user.type(within(dialog).getByRole('textbox'), 'Pi-ho')
    expect(confirmBtn).toBeDisabled()
    await user.type(within(dialog).getByRole('textbox'), 'le')
    expect(confirmBtn).toBeEnabled()
    await user.click(confirmBtn)
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE' && c.path === '/apps/pihole?purgeData=true')).toBe(true))
  })

  it('shows the empty state with a link to the store', async () => {
    mockApi([{ path: '/apps', body: [] }])
    renderApp(<AppsGrid />)
    expect(await screen.findByText('Nenhum app instalado ainda')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /loja de apps/i })).toHaveAttribute('href', '/store')
  })

  it('opens the log viewer for an app', async () => {
    mockApi([{ path: '/apps', body: APPS }])
    renderApp(<AppsGrid />)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Ações de Jellyfin' }))
    await user.click(screen.getByRole('menuitem', { name: 'Ver logs' }))
    const es = FakeEventSource.last('/apps/jellyfin/logs/stream')!
    expect(es).toBeDefined()
    es.emit('log', { stream: 'stdout', text: 'hello from jellyfin' })
    expect(await screen.findByText('hello from jellyfin')).toBeInTheDocument()
  })
})
