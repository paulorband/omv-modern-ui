import { useEffect, useRef, useState } from 'react'
import { subscribe } from '../lib/sse'

type Line = { stream: string; text: string }
const MAX_LINES = 1000

/** Follows a log SSE stream (`/containers/{id}/logs/stream` or `/apps/{id}/logs/stream`). */
export function LogViewer({ path }: { path: string }) {
  const [lines, setLines] = useState<Line[]>([])
  const [state, setState] = useState<'connecting' | 'live' | 'error'>('connecting')
  const box = useRef<HTMLPreElement>(null)
  const stick = useRef(true)

  useEffect(() => {
    setLines([])
    setState('connecting')
    return subscribe<Line & { message?: string }>(
      path,
      {
        log: (l) => {
          setState('live')
          setLines((prev) => [...prev.slice(-(MAX_LINES - 1)), l])
        },
        error: () => setState('error'),
      },
      () => setState('error'),
    )
  }, [path])

  useEffect(() => {
    const el = box.current
    if (el && stick.current) el.scrollTop = el.scrollHeight
  }, [lines])

  return (
    <div>
      <pre
        ref={box}
        tabIndex={0}
        aria-label="Logs"
        onScroll={(e) => {
          const el = e.currentTarget
          stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 24
        }}
        className="h-80 overflow-auto rounded-xl border border-line bg-black/80 p-3 font-mono text-xs leading-relaxed text-green-200"
      >
        {lines.length === 0 && <span className="text-muted">{state === 'error' ? 'Sem conexão com os logs.' : 'Aguardando logs…'}</span>}
        {lines.map((l, i) => (
          <div key={i} className={l.stream === 'stderr' ? 'text-amber-300' : undefined}>
            {l.text}
          </div>
        ))}
      </pre>
      <p className="mt-1 text-xs text-muted" role="status">
        {state === 'live' ? '● ao vivo' : state === 'error' ? 'conexão perdida — tentando reconectar' : 'conectando…'}
      </p>
    </div>
  )
}
