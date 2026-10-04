import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'
import { CheckCircle2, AlertTriangle } from 'lucide-react'

type Toast = { id: number; kind: 'ok' | 'error'; text: string }
type ToastApi = { ok: (text: string) => void; error: (text: string) => void }

const Ctx = createContext<ToastApi | null>(null)

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<Toast[]>([])
  const push = useCallback((kind: Toast['kind'], text: string) => {
    const id = Date.now() + Math.random()
    setItems((t) => [...t, { id, kind, text }])
    setTimeout(() => setItems((t) => t.filter((x) => x.id !== id)), kind === 'error' ? 7000 : 3500)
  }, [])
  const api = useMemo<ToastApi>(() => ({ ok: (t) => push('ok', t), error: (t) => push('error', t) }), [push])

  return (
    <Ctx.Provider value={api}>
      {children}
      <div className="pointer-events-none fixed inset-x-0 bottom-20 z-[60] flex flex-col items-center gap-2 px-4 sm:bottom-6" aria-live="polite">
        {items.map((t) => (
          <div
            key={t.id}
            role={t.kind === 'error' ? 'alert' : 'status'}
            className="pointer-events-auto flex max-w-md items-center gap-2 rounded-xl border border-line bg-surface-solid px-4 py-2.5 text-sm shadow-card"
          >
            {t.kind === 'ok' ? <CheckCircle2 className="h-4 w-4 text-ok" aria-hidden /> : <AlertTriangle className="h-4 w-4 text-danger" aria-hidden />}
            {t.text}
          </div>
        ))}
      </div>
    </Ctx.Provider>
  )
}

export function useToast(): ToastApi {
  const v = useContext(Ctx)
  if (!v) throw new Error('useToast must be used inside <ToastProvider>')
  return v
}
