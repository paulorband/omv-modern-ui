/** Parses a text/event-stream body into (event, data) pairs. Exported for tests. */
export function parseSSE(buffer: string): { events: { event: string; data: string }[]; rest: string } {
  const events: { event: string; data: string }[] = []
  const blocks = buffer.split('\n\n')
  const rest = blocks.pop() ?? ''
  for (const block of blocks) {
    let event = 'message'
    const data: string[] = []
    for (const line of block.split('\n')) {
      if (line.startsWith(':')) continue
      if (line.startsWith('event:')) event = line.slice(6).trim()
      else if (line.startsWith('data:')) data.push(line.slice(5).trimStart())
    }
    if (data.length) events.push({ event, data: data.join('\n') })
  }
  return { events, rest }
}

/** Subscribes to a GET event stream. Returns an unsubscribe function. */
export function subscribe<T>(
  path: string,
  handlers: Record<string, (data: T) => void>,
  onError?: () => void,
): () => void {
  const es = new EventSource('/api/v1' + path)
  for (const [name, fn] of Object.entries(handlers)) {
    es.addEventListener(name, (e) => {
      try {
        fn(JSON.parse((e as MessageEvent).data) as T)
      } catch {
        /* ignore malformed event */
      }
    })
  }
  es.onerror = () => onError?.()
  return () => es.close()
}

/** POST that answers with an event stream (EventSource only supports GET). */
export async function streamPost<T>(
  path: string,
  body: unknown,
  onEvent: (event: string, data: T) => void,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch('/api/v1' + path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'omv-home' },
    body: JSON.stringify(body),
    signal,
  })
  if (!res.ok || !res.body) {
    const j = await res.json().catch(() => null)
    throw new Error(j?.error?.message ?? `Erro ${res.status}`)
  }
  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buf = ''
  for (;;) {
    const { value, done } = await reader.read()
    if (done) break
    buf += decoder.decode(value, { stream: true })
    const { events, rest } = parseSSE(buf)
    buf = rest
    for (const ev of events) onEvent(ev.event, JSON.parse(ev.data) as T)
  }
}
