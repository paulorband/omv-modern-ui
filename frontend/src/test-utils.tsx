import type { ReactElement } from 'react'
import { render } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { vi } from 'vitest'
import { AuthProvider } from './lib/auth'
import { ToastProvider } from './components/Toast'

type Route = { method?: string; path: string; status?: number; body?: unknown }

/** Stubs fetch with canned JSON answers keyed by "METHOD /api/v1/path". Returns the recorded calls. */
export function mockApi(routes: Route[]) {
  const calls: { method: string; path: string; body?: unknown }[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input).replace(/^\/api\/v1/, '')
      const method = init?.method ?? 'GET'
      calls.push({ method, path: url, body: init?.body ? JSON.parse(String(init.body)) : undefined })
      const hit = routes.find((r) => (r.method ?? 'GET') === method && r.path === url)
      if (!hit) return new Response(JSON.stringify({ error: { code: 'not_found', message: `sem mock para ${method} ${url}` } }), { status: 404, headers: { 'content-type': 'application/json' } })
      const status = hit.status ?? 200
      if (status === 204) return new Response(null, { status })
      return new Response(JSON.stringify(hit.body ?? {}), { status, headers: { 'content-type': 'application/json' } })
    }),
  )
  return calls
}

export function renderApp(ui: ReactElement, { route = '/' }: { route?: string } = {}) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={client}>
      <MemoryRouter initialEntries={[route]}>
        <AuthProvider>
          <ToastProvider>{ui}</ToastProvider>
        </AuthProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

/** Minimal EventSource double: tests push events with `emit`. */
export class FakeEventSource {
  static instances: FakeEventSource[] = []
  listeners: Record<string, ((e: MessageEvent) => void)[]> = {}
  onerror: (() => void) | null = null
  closed = false
  constructor(public url: string) {
    FakeEventSource.instances.push(this)
  }
  addEventListener(name: string, fn: (e: MessageEvent) => void) {
    ;(this.listeners[name] ??= []).push(fn)
  }
  close() {
    this.closed = true
  }
  emit(name: string, data: unknown) {
    for (const fn of this.listeners[name] ?? []) fn(new MessageEvent(name, { data: JSON.stringify(data) }))
  }
  static reset() {
    FakeEventSource.instances = []
  }
  static last(urlPart: string) {
    return [...FakeEventSource.instances].reverse().find((i) => i.url.includes(urlPart))
  }
}
