import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Containers } from './Containers'
import { emptyForm, formToSpec, specToForm, validateForm } from '../lib/run'
import { FakeEventSource, mockApi, renderApp } from '../test-utils'
import type { Container, DockerImage } from '../lib/types'

const c = (over: Partial<Container>): Container => ({
  id: 'abc', name: 'web', image: 'nginx:alpine', state: 'running', status: 'Up 2 hours', ports: [{ private: 80, public: 8088, protocol: 'tcp' }], createdAt: '2026-01-01T00:00:00Z', ...over,
})

const LIST: Container[] = [
  c({ id: '1', name: 'nginx-test' }),
  c({ id: '2', name: 'jellyfin', image: 'jellyfin/jellyfin', appId: 'jellyfin', ports: [] }),
  c({ id: '3', name: 'postgres-old', image: 'postgres:15', state: 'exited', status: 'Exited (0)', ports: [] }),
]
const base = [
  { path: '/docker/status', body: { available: true, composeAvailable: true, version: '27.0.0' } },
  { path: '/containers', body: LIST },
]

beforeEach(() => {
  FakeEventSource.reset()
  vi.stubGlobal('EventSource', FakeEventSource)
})
afterEach(() => vi.unstubAllGlobals())

describe('run form helpers', () => {
  it('converts an imported spec to form state and back', () => {
    const spec = {
      image: 'nginx:alpine', name: 'web', restart: 'always',
      ports: [{ host: 8080, container: 80 }, { host: 0, container: 53, protocol: 'udp' }],
      volumes: [{ host: '/a', container: '/b', readOnly: true }],
      env: { A: '1' }, network: 'host', command: ['nginx', '-g', 'daemon off;'],
    }
    const form = specToForm(spec)
    expect(form.ports).toEqual([{ host: '8080', container: '80', protocol: 'tcp' }, { host: '', container: '53', protocol: 'udp' }])
    const back = formToSpec(form)
    expect(back.ports).toEqual([{ host: 8080, container: 80, protocol: 'tcp' }, { host: 0, container: 53, protocol: 'udp' }])
    expect(back.network).toBe('host')
    expect(back.command).toEqual(['nginx', '-g', 'daemon off;'])
    expect(back.env).toEqual({ A: '1' })
  })

  it('drops blank rows', () => {
    const f = { ...emptyForm(), image: ' img ', ports: [{ host: '', container: '', protocol: 'tcp' as const }], env: [{ key: ' ', value: 'x' }] }
    expect(formToSpec(f)).toEqual({ image: 'img', restart: 'unless-stopped' })
  })

  it('validates like the backend', () => {
    const ok = { ...emptyForm(), image: 'nginx' }
    expect(validateForm(ok)).toBeUndefined()
    expect(validateForm({ ...ok, image: '' })).toMatch(/imagem/)
    expect(validateForm({ ...ok, name: '../x' })).toMatch(/Nome inválido/)
    expect(validateForm({ ...ok, ports: [{ host: '1', container: '70000', protocol: 'tcp' }] })).toMatch(/Portas/)
    expect(validateForm({ ...ok, volumes: [{ host: '/a', container: 'rel', readOnly: false }] })).toMatch(/Volumes/)
    expect(validateForm({ ...ok, env: [{ key: '1BAD', value: '' }] })).toMatch(/Variável inválida/)
  })
})

describe('Containers page', () => {
  it('lists containers with state and published ports', async () => {
    mockApi(base)
    renderApp(<Containers />)
    expect(await screen.findByRole('button', { name: 'nginx-test' })).toBeInTheDocument()
    expect(screen.getByText('8088→80/tcp')).toBeInTheDocument()
    expect(screen.getAllByText('running')).toHaveLength(2)
    expect(screen.getByText('exited')).toBeInTheDocument()
  })

  it('stops a running container and starts a stopped one', async () => {
    const calls = mockApi([
      ...base,
      { method: 'POST', path: '/containers/nginx-test/stop', status: 204 },
      { method: 'POST', path: '/containers/postgres-old/start', status: 204 },
    ])
    renderApp(<Containers />)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Parar nginx-test' }))
    await user.click(screen.getByRole('button', { name: 'Iniciar postgres-old' }))
    await waitFor(() => {
      expect(calls.some((x) => x.path === '/containers/nginx-test/stop')).toBe(true)
      expect(calls.some((x) => x.path === '/containers/postgres-old/start')).toBe(true)
    })
  })

  it('does not allow removing app containers (A03.5 UI)', async () => {
    mockApi(base)
    renderApp(<Containers />)
    const btn = await screen.findByRole('button', { name: /jellyfin pertence ao app jellyfin/ })
    expect(btn).toBeDisabled()
  })

  it('removes a plain container, forcing when it is running', async () => {
    const calls = mockApi([...base, { method: 'DELETE', path: '/containers/nginx-test?force=true&volumes=false', status: 204 }])
    renderApp(<Containers />)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Remover nginx-test' }))
    const dialog = screen.getByRole('dialog', { name: 'Remover nginx-test' })
    await user.click(within(dialog).getByRole('button', { name: 'Remover' }))
    await waitFor(() => expect(calls.some((x) => x.method === 'DELETE' && x.path.includes('force=true'))).toBe(true))
  })

  it('imports a docker run command and creates the container', async () => {
    const calls = mockApi([
      ...base,
      {
        method: 'POST', path: '/containers/import-run',
        body: { image: 'nginx:alpine', name: 'x', ports: [{ host: 8080, container: 80 }], restart: 'always' },
      },
      { method: 'POST', path: '/containers/run', status: 201, body: { id: 'new' } },
    ])
    renderApp(<Containers />)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: /Novo container/ }))
    await user.click(screen.getByRole('tab', { name: 'Colar docker run' }))
    await user.type(screen.getByLabelText('Comando docker run'), 'docker run -d --name x -p 8080:80 nginx:alpine')
    await user.click(screen.getByRole('button', { name: 'Converter' }))

    expect(await screen.findByLabelText('Imagem')).toHaveValue('nginx:alpine')
    expect(screen.getByLabelText('Nome (opcional)')).toHaveValue('x')
    expect(screen.getByLabelText('Porta do servidor')).toHaveValue('8080')

    await user.click(screen.getByRole('button', { name: 'Criar e iniciar' }))
    await waitFor(() => expect(calls.find((x) => x.path === '/containers/run')).toBeDefined())
    expect(calls.find((x) => x.path === '/containers/run')!.body).toEqual({
      image: 'nginx:alpine', name: 'x', restart: 'always', ports: [{ host: 8080, container: 80, protocol: 'tcp' }],
    })
  })

  it('shows import errors from the backend', async () => {
    mockApi([...base, { method: 'POST', path: '/containers/import-run', status: 400, body: { error: { code: 'invalid_request', message: 'flags não suportadas: --rm' } } }])
    renderApp(<Containers />)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: /Novo container/ }))
    await user.click(screen.getByRole('tab', { name: 'Colar docker run' }))
    await user.type(screen.getByLabelText('Comando docker run'), 'docker run --rm img')
    await user.click(screen.getByRole('button', { name: 'Converter' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('--rm')
  })

  it('blocks creating a container with an invalid form', async () => {
    const calls = mockApi(base)
    renderApp(<Containers />)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: /Novo container/ }))
    await user.click(screen.getByRole('button', { name: 'Criar e iniciar' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Informe a imagem')
    expect(calls.some((x) => x.method === 'POST')).toBe(false)
  })

  it('disables creation when Docker is unavailable (R03.10)', async () => {
    mockApi([{ path: '/docker/status', body: { available: false, composeAvailable: false, error: 'Docker indisponível' } }, { path: '/containers', status: 502, body: { error: { code: 'upstream_error', message: 'Docker indisponível' } } }])
    renderApp(<Containers />)
    expect(await screen.findByText('O Docker não está disponível no servidor.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Novo container/ })).toBeDisabled()
  })

  it('lists images and protects those in use', async () => {
    const imgs: DockerImage[] = [
      { id: 'a1b2c3d4e5f6', tags: ['nginx:alpine'], size: 45 * 1024 * 1024, createdAt: '2026-01-01T00:00:00Z', inUse: true },
      { id: 'd4e5f6a1b2c3', tags: ['hello-world:latest'], size: 20 * 1024, createdAt: '2026-01-01T00:00:00Z', inUse: false },
    ]
    const calls = mockApi([...base, { path: '/images', body: imgs }, { method: 'DELETE', path: '/images/d4e5f6a1b2c3', status: 204 }])
    renderApp(<Containers />)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('tab', { name: 'Imagens' }))
    expect(await screen.findByText('nginx:alpine')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'nginx:alpine está em uso' })).toBeDisabled()
    await user.click(screen.getByRole('button', { name: 'Remover imagem hello-world:latest' }))
    await waitFor(() => expect(calls.some((x) => x.method === 'DELETE' && x.path === '/images/d4e5f6a1b2c3')).toBe(true))
  })
})
