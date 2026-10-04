export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

const UNAUTHORIZED_EVENT = 'omvhome:unauthorized'

/** Fired when any call (other than login) answers 401, so the app can return to the login page. */
export function onUnauthorized(handler: () => void): () => void {
  window.addEventListener(UNAUTHORIZED_EVENT, handler)
  return () => window.removeEventListener(UNAUTHORIZED_EVENT, handler)
}

type Options = { method?: string; body?: unknown; signal?: AbortSignal }

export async function api<T = void>(path: string, opts: Options = {}): Promise<T> {
  const method = opts.method ?? 'GET'
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (method !== 'GET') headers['X-Requested-With'] = 'omv-home'
  let body: string | undefined
  if (opts.body !== undefined) {
    headers['Content-Type'] = 'application/json'
    body = JSON.stringify(opts.body)
  }

  let res: Response
  try {
    res = await fetch('/api/v1' + path, { method, headers, body, signal: opts.signal, credentials: 'same-origin' })
  } catch (e) {
    if (e instanceof DOMException && e.name === 'AbortError') throw e
    throw new ApiError(0, 'network', 'Não foi possível falar com o servidor.')
  }

  if (res.status === 204) return undefined as T
  const isJson = res.headers.get('content-type')?.includes('application/json')
  const payload = isJson ? await res.json().catch(() => null) : await res.text()

  if (!res.ok) {
    if (res.status === 401 && path !== '/auth/login') window.dispatchEvent(new Event(UNAUTHORIZED_EVENT))
    const err = (payload as { error?: { code: string; message: string } } | null)?.error
    throw new ApiError(res.status, err?.code ?? 'error', err?.message ?? `Erro ${res.status}`)
  }
  return payload as T
}

export const get = <T>(path: string, signal?: AbortSignal) => api<T>(path, { signal })
export const post = <T = void>(path: string, body?: unknown) => api<T>(path, { method: 'POST', body: body ?? {} })
export const patch = <T = void>(path: string, body: unknown) => api<T>(path, { method: 'PATCH', body })
export const del = <T = void>(path: string) => api<T>(path, { method: 'DELETE' })
