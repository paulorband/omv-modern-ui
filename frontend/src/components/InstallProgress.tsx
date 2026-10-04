import { useEffect, useRef, useState } from 'react'
import { CheckCircle2, XCircle } from 'lucide-react'
import { subscribe } from '../lib/sse'
import type { InstallEvent } from '../lib/types'
import { Spinner } from './ui'

/** Follows `/store/installs/{id}/events` until the install finishes (R05.9). */
export function InstallProgress({ appId, onDone }: { appId: string; onDone: () => void }) {
  const [lines, setLines] = useState<string[]>([])
  const [status, setStatus] = useState<'running' | 'done' | 'error'>('running')
  const [error, setError] = useState('')
  const box = useRef<HTMLPreElement>(null)
  const finished = useRef(false)
  const onDoneRef = useRef(onDone)
  onDoneRef.current = onDone

  useEffect(() => {
    finished.current = false
    let unsubscribe: () => void = () => {}
    const finish = (s: 'done' | 'error') => {
      finished.current = true
      setStatus(s)
      unsubscribe() // the server closes the stream: stop EventSource from replaying it on reconnect
      if (s === 'done') onDoneRef.current()
    }
    const add = (e: InstallEvent) => e.message && setLines((l) => [...l.slice(-300), e.message!])
    unsubscribe = subscribe<InstallEvent>(
      `/store/installs/${appId}/events`,
      {
        starting: add,
        pull: add,
        log: add,
        done: (e) => {
          add(e)
          finish('done')
        },
        error: (e) => {
          setError(e.message ?? 'Falha na instalação')
          finish('error')
        },
      },
      () => {
        if (!finished.current) unsubscribe()
      },
    )
    return () => unsubscribe()
  }, [appId])

  useEffect(() => {
    if (box.current) box.current.scrollTop = box.current.scrollHeight
  }, [lines])

  return (
    <div className="space-y-3" aria-live="polite">
      {status === 'running' && <Spinner label="Instalando… isso pode levar alguns minutos (download das imagens)." />}
      {status === 'done' && (
        <p className="flex items-center gap-2 font-medium text-ok">
          <CheckCircle2 className="h-5 w-5" aria-hidden /> Instalação concluída!
        </p>
      )}
      {status === 'error' && (
        <p role="alert" className="flex items-start gap-2 font-medium text-danger">
          <XCircle className="mt-0.5 h-5 w-5 shrink-0" aria-hidden /> <span>Não foi possível instalar: {error}</span>
        </p>
      )}
      <pre ref={box} aria-label="Progresso da instalação" className="h-56 overflow-auto rounded-xl border border-line bg-black/80 p-3 font-mono text-xs text-green-200">
        {lines.length === 0 ? <span className="text-muted">Aguardando…</span> : lines.map((l, i) => <div key={i}>{l}</div>)}
      </pre>
    </div>
  )
}
