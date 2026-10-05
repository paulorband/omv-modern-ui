import { useEffect, useRef, useState } from 'react'
import { Link } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { AlertTriangle, CheckCircle2, ChevronDown, Download, Loader2, X } from 'lucide-react'
import { del, get } from '../lib/api'
import type { InstallJob } from '../lib/types'
import { AppIcon } from './AppIcon'
import { InstallProgress } from './InstallProgress'
import { useToast } from './Toast'
import { Button, Card, Modal, cx, errorMessage } from './ui'

/** Finished successful installs leave the widget after this long (R10.10). */
export const DONE_VISIBLE_MS = 60_000
const FAST_POLL_MS = 2000
const SLOW_POLL_MS = 15_000

/** Installs to show: running, failed, and recently finished ones. Pure, for tests. */
export function visibleInstalls(jobs: InstallJob[], now: number): InstallJob[] {
  return jobs.filter((j) => j.state !== 'done' || now - Date.parse(j.finishedAt ?? j.startedAt) < DONE_VISIBLE_MS)
}

export function useInstalls() {
  return useQuery({
    queryKey: ['installs'],
    queryFn: ({ signal }) => get<InstallJob[]>('/store/installs', signal),
    refetchInterval: (q) => (q.state.data?.some((j) => j.state === 'running') ? FAST_POLL_MS : SLOW_POLL_MS),
  })
}

/** Global install tracker: several installs at once, full log on demand, kept after failures (SPEC-10). */
export function InstallsWidget() {
  const qc = useQueryClient()
  const toast = useToast()
  const installs = useInstalls()
  const [open, setOpen] = useState(false)
  const [logId, setLogId] = useState<string | null>(null)
  const [now, setNow] = useState(() => Date.now())
  const seen = useRef<Map<string, InstallJob['state']> | null>(null)

  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 5000)
    return () => clearInterval(t)
  }, [])

  // running → done/error transitions: tell the user and refresh what changed (R10.11)
  useEffect(() => {
    const jobs = installs.data
    if (!jobs) return
    const prev = seen.current
    seen.current = new Map(jobs.map((j) => [j.id, j.state]))
    if (!prev) return
    for (const j of jobs) {
      if (prev.get(j.id) !== 'running') continue
      if (j.state === 'done') {
        toast.ok(`${j.title} foi instalado.`)
        void qc.invalidateQueries({ queryKey: ['apps'] })
        void qc.invalidateQueries({ queryKey: ['store'] })
      } else if (j.state === 'error') {
        toast.error(`Falha ao instalar ${j.title}. Veja o log no painel de instalações.`)
        setOpen(true)
      }
    }
  }, [installs.data, qc, toast])

  const dismiss = useMutation({
    mutationFn: (id: string) => del(`/store/installs/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: ['installs'] }),
    onError: (e) => toast.error(errorMessage(e)),
  })

  const jobs = visibleInstalls(installs.data ?? [], now)
  if (jobs.length === 0) return null
  const running = jobs.filter((j) => j.state === 'running').length
  const failed = jobs.filter((j) => j.state === 'error').length
  const logJob = (installs.data ?? []).find((j) => j.id === logId)

  const summary = [
    running > 0 && `${running} instalando`,
    failed > 0 && `${failed} ${failed === 1 ? 'falhou' : 'falharam'}`,
    running === 0 && failed === 0 && 'Instalação concluída',
  ]
    .filter(Boolean)
    .join(' · ')

  return (
    <>
      <section aria-label="Instalações" className="fixed bottom-20 right-4 z-50 w-[min(22rem,calc(100vw-2rem))] sm:bottom-4">
        {open ? (
          <Card className="overflow-hidden bg-surface-solid">
            <header className="flex items-center justify-between border-b border-line px-3 py-2">
              <h2 className="flex items-center gap-2 text-sm font-semibold">
                <Download className="h-4 w-4" aria-hidden /> Instalações
              </h2>
              <button type="button" aria-label="Recolher painel de instalações" onClick={() => setOpen(false)} className="rounded-lg p-1 text-muted hover:bg-surface-2 hover:text-fg">
                <ChevronDown className="h-4 w-4" aria-hidden />
              </button>
            </header>
            <ul className="max-h-80 divide-y divide-line overflow-auto">
              {jobs.map((j) => (
                <li key={j.id} className="flex items-center gap-2 px-3 py-2">
                  <button type="button" aria-label={`Ver log de ${j.title}`} onClick={() => setLogId(j.id)} className="flex min-w-0 flex-1 items-center gap-3 text-left">
                    <AppIcon icon={j.icon} name={j.title} className="h-9 w-9 rounded-xl text-xl" />
                    <span className="min-w-0 flex-1">
                      <span className="flex items-center gap-1.5 text-sm font-medium">
                        <span className="truncate">{j.title}</span>
                        <StateIcon state={j.state} />
                      </span>
                      <span className={cx('block truncate text-xs', j.state === 'error' ? 'text-danger' : 'text-muted')}>{j.message || 'Aguardando…'}</span>
                    </span>
                  </button>
                  {j.state === 'done' && (
                    <Link to={`/?highlight=${j.id}`} className="shrink-0 text-xs font-medium text-accent hover:underline">
                      Abrir
                    </Link>
                  )}
                  {j.state !== 'running' && (
                    <button type="button" aria-label={`Dispensar ${j.title}`} onClick={() => dismiss.mutate(j.id)} className="shrink-0 rounded-lg p-1 text-muted hover:bg-surface-2 hover:text-fg">
                      <X className="h-4 w-4" aria-hidden />
                    </button>
                  )}
                </li>
              ))}
            </ul>
          </Card>
        ) : (
          <button
            type="button"
            onClick={() => setOpen(true)}
            aria-label={`Instalações: ${summary}`}
            className={cx(
              'ml-auto flex items-center gap-2 rounded-full border bg-surface-solid px-4 py-2 text-sm font-medium shadow-card',
              failed > 0 ? 'border-danger/50 text-danger' : 'border-line',
            )}
          >
            {running > 0 ? <Loader2 className="h-4 w-4 animate-spin" aria-hidden /> : failed > 0 ? <AlertTriangle className="h-4 w-4" aria-hidden /> : <CheckCircle2 className="h-4 w-4 text-ok" aria-hidden />}
            {summary}
          </button>
        )}
      </section>

      {logJob && (
        <Modal open wide onClose={() => setLogId(null)} title={`Instalação de ${logJob.title}`} footer={<Button onClick={() => setLogId(null)}>Fechar</Button>}>
          <InstallProgress appId={logJob.id} onDone={() => {}} />
        </Modal>
      )}
    </>
  )
}

function StateIcon({ state }: { state: InstallJob['state'] }) {
  if (state === 'running') return <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-muted" aria-label="Em andamento" />
  if (state === 'done') return <CheckCircle2 className="h-3.5 w-3.5 shrink-0 text-ok" aria-label="Concluída" />
  return <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-danger" aria-label="Falhou" />
}
