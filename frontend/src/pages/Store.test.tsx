import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Route, Routes } from 'react-router-dom'
import { Store, filterStore, groupStore } from './Store'
import { StoreApp } from './StoreApp'
import { InstallsWidget } from '../components/InstallsWidget'
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

describe('custom sources (SPEC-09)', () => {
  const SRC = { id: 'umbrel', name: 'Denny Umbrel', url: 'https://github.com/dennysubke/dennys-umbrel-app-store', format: 'umbrel', apps: 328, updatedAt: '2026-10-05T10:00:00Z' }
  const EXTERNAL: StoreCard[] = [
    ...CARDS,
    { id: 'denny-actualbudget', title: 'Actual Budget', tagline: 'Finanças', icon: '', category: 'Finance', installed: false, source: 'umbrel' },
    { id: 'denny-ente', title: 'Ente', tagline: 'Fotos', icon: '', category: 'Photos', installed: false, source: 'umbrel', unsupported: true },
  ]

  it('filters by source (R09.9)', () => {
    expect(filterStore(EXTERNAL, '', 'Todos', 'umbrel').map((c) => c.id)).toEqual(['denny-actualbudget', 'denny-ente'])
    expect(filterStore(EXTERNAL, '', 'Todos', 'builtin')).toHaveLength(3)
    expect(filterStore(EXTERNAL, '', 'Todos', 'Todos')).toHaveLength(5)
  })

  it('shows the source name, an incompatible badge, and the source filter', async () => {
    mockApi([{ path: '/store/apps', body: EXTERNAL }, { path: '/store/sources', body: [SRC] }])
    renderApp(<Store />)
    const user = userEvent.setup()
    expect(await screen.findByText('Actual Budget')).toBeInTheDocument()
    expect(screen.getAllByText('Denny Umbrel').length).toBeGreaterThan(0)
    expect(screen.getByText('Incompatível')).toBeInTheDocument()
    await user.selectOptions(screen.getByLabelText('Fonte'), 'umbrel')
    expect(screen.queryByText('Nextcloud')).not.toBeInTheDocument()
    expect(screen.getByText('Ente')).toBeInTheDocument()
  })

  it('hides the source filter when there are no sources', async () => {
    mockApi([{ path: '/store/apps', body: CARDS }, { path: '/store/sources', body: [] }])
    renderApp(<Store />)
    await screen.findByText('Nextcloud')
    expect(screen.queryByLabelText('Fonte')).not.toBeInTheDocument()
  })

  it('adds a source by URL and shows backend errors (A09.8)', async () => {
    const calls = mockApi([
      { path: '/store/apps', body: CARDS },
      { path: '/store/sources', body: [] },
      { method: 'POST', path: '/store/sources', status: 400, body: { error: { code: 'invalid_request', message: 'formato não reconhecido' } } },
    ])
    renderApp(<Store />)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: /Fontes/ }))
    const dialog = await screen.findByRole('dialog', { name: 'Fontes de apps' })
    expect(await within(dialog).findByText('Nenhuma fonte adicionada ainda.')).toBeInTheDocument()
    expect(within(dialog).getByRole('button', { name: 'Adicionar fonte' })).toBeDisabled()
    await user.type(within(dialog).getByLabelText('Endereço da fonte'), 'https://github.com/x/y')
    await user.click(within(dialog).getByRole('button', { name: 'Adicionar fonte' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('formato não reconhecido')
    expect(calls.find((c) => c.method === 'POST')!.body).toEqual({ url: 'https://github.com/x/y', name: '' })
  })

  it('lists, refreshes and removes sources', async () => {
    const calls = mockApi([
      { path: '/store/apps', body: CARDS },
      { path: '/store/sources', body: [SRC] },
      { method: 'POST', path: '/store/sources/umbrel/refresh', body: SRC },
      { method: 'DELETE', path: '/store/sources/umbrel', status: 204 },
    ])
    renderApp(<Store />)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: /Fontes/ }))
    const dialog = await screen.findByRole('dialog', { name: 'Fontes de apps' })
    expect(await within(dialog).findByText('Denny Umbrel')).toBeInTheDocument()
    expect(within(dialog).getByText('328 apps', { exact: false })).toBeInTheDocument()

    await user.click(within(dialog).getByRole('button', { name: 'Atualizar Denny Umbrel' }))
    await waitFor(() => expect(calls.some((c) => c.path === '/store/sources/umbrel/refresh')).toBe(true))

    await user.click(within(dialog).getByRole('button', { name: 'Remover Denny Umbrel' }))
    expect(calls.some((c) => c.method === 'DELETE')).toBe(false) // needs confirmation
    await user.click(within(dialog).getByRole('button', { name: 'Remover Denny Umbrel' }))
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE')).toBe(true))
  })

  describe('source app detail', () => {
    const detail = (over: Partial<StoreDetail>): StoreDetail => ({ ...DETAIL, source: 'umbrel', sourceName: 'Denny Umbrel', unsafe: [], warnings: [], ...over })
    const renderDetail = () =>
      renderApp(
        <Routes>
          <Route path="/store/:id" element={<StoreApp />} />
        </Routes>,
        { route: '/store/qbittorrent' },
      )
    const dockerOk = { path: '/docker/status', body: { available: true, composeAvailable: true } }

    it('blocks incompatible apps and says why', async () => {
      mockApi([{ path: '/store/apps/qbittorrent', body: detail({ unsupported: 'depende de outros apps do Umbrel (bitcoin)' }) }, dockerOk])
      renderDetail()
      expect(await screen.findByText(/depende de outros apps do Umbrel/)).toBeInTheDocument()
      expect(screen.getByText('Fonte: Denny Umbrel')).toBeInTheDocument()
      expect(screen.getByRole('button', { name: /Instalar qBittorrent/ })).toBeDisabled()
    })

    it('requires the unsafe checkbox and sends allowUnsafe', async () => {
      const calls = mockApi([
        { path: '/store/apps/qbittorrent', body: detail({ unsafe: ['web: privileged: true'], warnings: ['web: capacidades extras (NET_ADMIN)'] }) },
        dockerOk,
        { method: 'POST', path: '/store/apps/qbittorrent/install', status: 202, body: { jobId: 'qbittorrent' } },
      ])
      renderDetail()
      const user = userEvent.setup()
      const install = await screen.findByRole('button', { name: /Instalar qBittorrent/ })
      expect(screen.getByText(/capacidades extras/)).toBeInTheDocument()
      expect(install).toBeDisabled()
      await user.click(screen.getByRole('checkbox', { name: /Permitir configurações inseguras/ }))
      expect(install).toBeEnabled()
      await user.click(install)
      await waitFor(() => expect(calls.find((c) => c.method === 'POST')).toBeDefined())
      expect(calls.find((c) => c.method === 'POST')!.body).toMatchObject({ allowUnsafe: true })
    })
  })
})

describe('install starts refresh the global widget (R10.12)', () => {
  it('refetches the installs list right after the POST', async () => {
    const routes = [
      { path: '/store/apps/qbittorrent', body: DETAIL },
      { path: '/docker/status', body: { available: true, composeAvailable: true } },
      { path: '/store/installs', body: [] },
      { method: 'POST', path: '/store/apps/qbittorrent/install', status: 202, body: { jobId: 'qbittorrent' } },
    ]
    const calls = mockApi(routes)
    renderApp(
      <>
        <InstallsWidget />
        <Routes>
          <Route path="/store/:id" element={<StoreApp />} />
        </Routes>
      </>,
      { route: '/store/qbittorrent' },
    )
    const user = userEvent.setup()
    await waitFor(() => expect(calls.filter((c) => c.path === '/store/installs')).toHaveLength(1))
    await user.click(await screen.findByRole('button', { name: /Instalar qBittorrent/ }))
    await waitFor(() => expect(calls.filter((c) => c.path === '/store/installs')).toHaveLength(2))
  })
})

describe('Store sources grouping, URL filters and compose editing', () => {
  const SOURCES = [{ id: 'umbrel', name: 'Umbrel' }, { id: 'casa', name: 'CasaOS' }]
  const DUP: StoreCard[] = [
    { id: 'jellyfin-umbrel', group: 'jellyfin', title: 'Jellyfin', tagline: '', icon: '', category: 'Media', installed: false, source: 'umbrel' },
    { id: 'jellyfin', group: 'jellyfin', title: 'Jellyfin', tagline: '', icon: '', category: 'Media', installed: false },
    { id: 'jellyfin-casa', group: 'jellyfin', title: 'Jellyfin', tagline: '', icon: '', category: 'Media', installed: false, source: 'casa' },
    { id: 'pihole', group: 'pihole', title: 'Pi-hole', tagline: '', icon: '', category: 'Network', installed: false, source: 'casa' },
  ]

  it('groupStore puts the built-in app first and keeps others in order', () => {
    const groups = groupStore(DUP)
    expect(groups.map((g) => g.id)).toEqual(['jellyfin', 'pihole'])
    expect(groups[0].primary.id).toBe('jellyfin')
    expect(groups[0].cards.map((c) => c.id)).toEqual(['jellyfin', 'jellyfin-umbrel', 'jellyfin-casa'])
  })

  it('shows one card per app with a badge per source', async () => {
    mockApi([{ path: '/store/apps', body: DUP }, { path: '/store/sources', body: SOURCES }])
    renderApp(<Store />)
    await screen.findByText('Pi-hole')
    expect(screen.getAllByText('Jellyfin')).toHaveLength(1)
    const card = screen.getByRole('link', { name: /Jellyfin/ })
    expect(card).toHaveAttribute('href', '/store/jellyfin')
    for (const name of ['OMV Home', 'Umbrel', 'CasaOS']) expect(within(card).getByText(name)).toBeInTheDocument()
  })

  it('restores filters from the URL', async () => {
    mockApi([{ path: '/store/apps', body: CARDS }])
    renderApp(<Store />, { route: '/store?category=Files' })
    expect(await screen.findByText('Nextcloud')).toBeInTheDocument()
    expect(screen.queryByText('Jellyfin')).not.toBeInTheDocument()
  })

  it('detail back link keeps the store filters', async () => {
    mockApi([{ path: '/store/apps', body: CARDS }, { path: '/store/apps/nextcloud', body: { ...DETAIL, meta: { ...DETAIL.meta, id: 'nextcloud', env: [] } } }, { path: '/docker/status', body: { available: true, composeAvailable: true } }])
    renderApp(
      <Routes>
        <Route path="/store" element={<Store />} />
        <Route path="/store/:id" element={<StoreApp />} />
      </Routes>,
      { route: '/store?category=Files' },
    )
    const user = userEvent.setup()
    await user.click(await screen.findByRole('link', { name: /Nextcloud/ }))
    const back = await screen.findByRole('link', { name: /Voltar para a loja/ })
    expect(back).toHaveAttribute('href', '/store?category=Files')
    await user.click(back)
    expect(await screen.findByText('Nextcloud')).toBeInTheDocument()
    expect(screen.queryByText('Jellyfin')).not.toBeInTheDocument()
  })

  it('detail switches between sources and installs an edited compose', async () => {
    const detail = { ...DETAIL, installed: false, compose: 'services:\n  a:\n    image: x\n', variants: [{ id: 'qbittorrent', source: '' }, { id: 'qbittorrent-umbrel', source: 'umbrel' }] }
    const calls = mockApi([
      { path: '/store/apps/qbittorrent', body: detail },
      { path: '/store/sources', body: SOURCES },
      { path: '/docker/status', body: { available: true, composeAvailable: true } },
      { method: 'POST', path: '/store/apps/qbittorrent/install', status: 202, body: { jobId: 'qbittorrent' } },
    ])
    renderApp(
      <Routes>
        <Route path="/store/:id" element={<StoreApp />} />
      </Routes>,
      { route: '/store/qbittorrent' },
    )
    const user = userEvent.setup()
    expect(await screen.findByRole('link', { name: 'Umbrel' })).toHaveAttribute('href', '/store/qbittorrent-umbrel')
    expect(screen.getByRole('link', { name: 'OMV Home' })).toHaveAttribute('aria-current', 'true')

    await user.click(screen.getByRole('button', { name: 'Editar' }))
    const box = screen.getByLabelText('docker-compose.yml')
    await user.type(box, '# meu ajuste')
    await user.click(screen.getByRole('button', { name: /Instalar qBittorrent/ }))
    await waitFor(() => expect(calls.some((c) => c.method === 'POST')).toBe(true))
    expect((calls.find((c) => c.method === 'POST')!.body as { compose: string }).compose).toContain('# meu ajuste')
  })
})
