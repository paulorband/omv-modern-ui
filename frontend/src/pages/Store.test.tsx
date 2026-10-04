import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes } from 'react-router-dom'
import { Store, filterStore } from './Store'
import { StoreApp } from './StoreApp'
import { validateField, validatePort } from '../lib/install'
import { FakeEventSource, mockApi, renderApp } from '../test-utils'
import type { EnvField, StoreCard, StoreDetail } from '../lib/types'

const CARDS: StoreCard[] = [
  { id: 'jellyfin', title: 'Jellyfin', tagline: 'Servidor de mídia', icon: 'emoji:🎬', category: 'Media', installed: true },
  { id: 'nextcloud', title: 'Nextcloud', tagline: 'Sua nuvem pessoal', icon: 'emoji:☁️', category: 'Files', installed: false },
  { id: 'uptime-kuma', title: 'Uptime Kuma', tagline: 'Monitoramento', icon: '', category: 'Monitoring', installed: false },
]

const DETAIL: StoreDetail = {
  installed: false,
  compose: 'services: {}',
  meta: {
    id: 'qbittorrent', title: 'qBittorrent', tagline: 'Torrents', description: 'Cliente de torrents.', icon: 'emoji:⬇️', category: 'Downloads', port: 8083, main: 'qbittorrent',
    env: [{ key: 'DOWNLOADS_DIR', label: 'Pasta de downloads', type: 'path', default: '/srv/downloads', required: true }],
  },
}

beforeEach(() => {
  FakeEventSource.reset()
  vi.stubGlobal('EventSource', FakeEventSource)
})
afterEach(() => vi.unstubAllGlobals())

describe('validateField (R05.6)', () => {
  const f = (over: Partial<EnvField>): EnvField => ({ key: 'K', label: 'K', type: 'string', default: '', required: false, ...over })
  it('requires required fields', () => {
    expect(validateField(f({ required: true }), '  ')).toBe('Campo obrigatório')
    expect(validateField(f({}), '')).toBeUndefined()
  })
  it('validates paths', () => {
    expect(validateField(f({ type: 'path' }), 'relative')).toMatch(/absoluto/)
    expect(validateField(f({ type: 'path' }), '/a/../b')).toMatch(/\.\./)
    expect(validateField(f({ type: 'path' }), '/srv/media')).toBeUndefined()
  })
  it('validates ports, selects and multi-line values', () => {
    expect(validateField(f({ type: 'port' }), '70000')).toBeDefined()
    expect(validateField(f({ type: 'port' }), '8080')).toBeUndefined()
    expect(validateField(f({ type: 'select', options: ['a', 'b'] }), 'c')).toBeDefined()
    expect(validateField(f({}), 'a\nb')).toBe('Valor inválido')
    expect(validatePort('')).toBeUndefined()
    expect(validatePort('0')).toBeDefined()
  })
})

describe('Store page', () => {
  it('lists catalog apps and marks installed ones', async () => {
    mockApi([{ path: '/store/apps', body: CARDS }])
    renderApp(<Store />)
    expect(await screen.findByText('Nextcloud')).toBeInTheDocument()
    expect(screen.getByText('Instalado')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: /Nextcloud/ })).toHaveAttribute('href', '/store/nextcloud')
  })

  it('filters by text and category', async () => {
    mockApi([{ path: '/store/apps', body: CARDS }])
    renderApp(<Store />)
    const user = userEvent.setup()
    await screen.findByText('Nextcloud')
    await user.type(screen.getByLabelText('Buscar na loja'), 'monit')
    expect(screen.queryByText('Nextcloud')).not.toBeInTheDocument()
    expect(screen.getByText('Uptime Kuma')).toBeInTheDocument()
    expect(filterStore(CARDS, '', 'Files').map((c) => c.id)).toEqual(['nextcloud'])
  })

  it('shows the unsafe checkbox only after the backend asks for it', async () => {
    const calls = mockApi([
      { path: '/store/apps', body: CARDS },
      { method: 'POST', path: '/store/custom', status: 400, body: { error: { code: 'invalid_request', message: 'configurações inseguras exigem allowUnsafe: app: privileged: true' } } },
    ])
    renderApp(<Store />)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: /App personalizado/ }))
    expect(screen.queryByText(/Permitir configurações inseguras/)).not.toBeInTheDocument()
    await user.type(screen.getByPlaceholderText('Meu app'), 'Teste')
    await user.click(screen.getByRole('button', { name: 'Instalar' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('allowUnsafe')
    expect(screen.getByText(/Permitir configurações inseguras/)).toBeInTheDocument()
    expect(calls.filter((c) => c.method === 'POST')).toHaveLength(1)
  })
})

describe('Store detail + install flow (A05.x UI)', () => {
  const routes = [
    { path: '/store/apps/qbittorrent', body: DETAIL },
    { path: '/docker/status', body: { available: true, composeAvailable: true } },
  ]
  const renderDetail = () =>
    renderApp(
      <Routes>
        <Route path="/store/:id" element={<StoreApp />} />
        <Route path="/" element={<p>home</p>} />
      </Routes>,
      { route: '/store/qbittorrent' },
    )

  it('renders the form from the template and blocks invalid paths', async () => {
    const calls = mockApi(routes)
    renderDetail()
    const user = userEvent.setup()
    const dir = await screen.findByLabelText('Pasta de downloads')
    expect(dir).toHaveValue('/srv/downloads')
    expect(screen.getByLabelText('Porta de acesso')).toHaveValue('8083')

    await user.clear(dir)
    await user.type(dir, 'relativo')
    await user.click(screen.getByRole('button', { name: /Instalar qBittorrent/ }))
    expect(await screen.findByText(/caminho absoluto/)).toBeInTheDocument()
    expect(calls.some((c) => c.method === 'POST')).toBe(false)
  })

  it('installs, streams progress, and refreshes apps when done', async () => {
    const calls = mockApi([...routes, { method: 'POST', path: '/store/apps/qbittorrent/install', status: 202, body: { jobId: 'qbittorrent' } }])
    renderDetail()
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: /Instalar qBittorrent/ }))

    await waitFor(() => expect(FakeEventSource.last('/store/installs/qbittorrent/events')).toBeDefined())
    const post = calls.find((c) => c.method === 'POST')!
    expect(post.body).toEqual({ env: { DOWNLOADS_DIR: '/srv/downloads' }, port: 8083 })

    const es = FakeEventSource.last('/store/installs/qbittorrent/events')!
    act(() => es.emit('starting', { type: 'starting', message: 'Iniciando instalação' }))
    act(() => es.emit('pull', { type: 'pull', message: 'Pulling qbittorrent' }))
    expect(await screen.findByText('Pulling qbittorrent')).toBeInTheDocument()
    act(() => es.emit('done', { type: 'done', message: 'Instalação concluída' }))
    expect(await screen.findByText('Instalação concluída!')).toBeInTheDocument()
    expect(es.closed).toBe(true) // must not reconnect and replay the finished job
  })

  it('shows the backend error when the install fails', async () => {
    mockApi([...routes, { method: 'POST', path: '/store/apps/qbittorrent/install', status: 409, body: { error: { code: 'conflict', message: 'a porta 8083 já está em uso pelo container "x"' } } }])
    renderDetail()
    await userEvent.setup().click(await screen.findByRole('button', { name: /Instalar qBittorrent/ }))
    expect(await screen.findByRole('alert')).toHaveTextContent('a porta 8083 já está em uso')
  })

  it('shows install failures from the event stream', async () => {
    mockApi([...routes, { method: 'POST', path: '/store/apps/qbittorrent/install', status: 202, body: { jobId: 'qbittorrent' } }])
    renderDetail()
    await userEvent.setup().click(await screen.findByRole('button', { name: /Instalar qBittorrent/ }))
    await waitFor(() => expect(FakeEventSource.last('/events')).toBeDefined())
    act(() => FakeEventSource.last('/events')!.emit('error', { type: 'error', message: 'manifest unknown' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('manifest unknown')
  })

  it('disables install when docker compose is missing (R08.7)', async () => {
    mockApi([{ path: '/store/apps/qbittorrent', body: DETAIL }, { path: '/docker/status', body: { available: true, composeAvailable: false } }])
    renderDetail()
    expect(await screen.findByText(/docker compose não está instalado/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Instalar qBittorrent/ })).toBeDisabled()
  })

  it('disables install for already installed apps', async () => {
    mockApi([{ path: '/store/apps/qbittorrent', body: { ...DETAIL, installed: true } }, { path: '/docker/status', body: { available: true, composeAvailable: true } }])
    renderDetail()
    expect(await screen.findByText(/já está instalado/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Instalar qBittorrent/ })).toBeDisabled()
  })
})
