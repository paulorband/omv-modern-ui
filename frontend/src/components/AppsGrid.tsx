import { useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Search, Store } from 'lucide-react'
import { del, get, post } from '../lib/api'
import type { App } from '../lib/types'
import { AppCard, type AppAction } from './AppCard'
import { EditAppDialog } from './EditAppDialog'
import { LogViewer } from './LogViewer'
import { useToast } from './Toast'
import { Button, EmptyState, ErrorBox, Field, Modal, Spinner, cx, errorMessage, inputCls } from './ui'

const ALL = 'Todos'

/** Pure filter used by the grid (A04.5). */
export function filterApps(apps: App[], query: string, category: string): App[] {
  const q = query.trim().toLowerCase()
  return apps.filter((a) => (category === ALL || a.category === category) && (!q || a.name.toLowerCase().includes(q) || a.id.includes(q)))
}

export function AppsGrid() {
  const qc = useQueryClient()
  const toast = useToast()
  const highlight = useSearchParams()[0].get('highlight')
  const [query, setQuery] = useState('')
  const [category, setCategory] = useState(ALL)
  const [logsFor, setLogsFor] = useState<App | null>(null)
  const [removing, setRemoving] = useState<App | null>(null)
  const [editing, setEditing] = useState<App | null>(null)

  const apps = useQuery({ queryKey: ['apps'], queryFn: ({ signal }) => get<App[]>('/apps', signal), refetchInterval: 10_000 })

  const control = useMutation({
    mutationFn: ({ app, action }: { app: App; action: 'start' | 'stop' | 'restart' | 'update' }) => post(`/apps/${app.id}/${action}`),
    onSuccess: (_d, { app, action }) => {
      const verb = { start: 'iniciado', stop: 'parado', restart: 'reiniciado', update: 'atualizado' }[action]
      toast.ok(`${app.name} ${verb}.`)
    },
    onError: (e, { app }) => toast.error(`${app.name}: ${errorMessage(e)}`),
    onSettled: () => void qc.invalidateQueries({ queryKey: ['apps'] }),
  })

  const categories = useMemo(() => [ALL, ...Array.from(new Set((apps.data ?? []).map((a) => a.category))).sort()], [apps.data])
  const visible = useMemo(() => filterApps(apps.data ?? [], query, category), [apps.data, query, category])

  const onAction = (app: App, a: AppAction) => {
    if (a === 'logs') setLogsFor(app)
    else if (a === 'remove') setRemoving(app)
    else if (a === 'edit') setEditing(app)
    else control.mutate({ app, action: a })
  }
  const busyId = control.isPending ? control.variables?.app.id : undefined

  if (apps.isPending) return <Spinner />
  if (apps.isError) return <ErrorBox error={apps.error} onRetry={() => void apps.refetch()} />

  return (
    <section aria-label="Apps">
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <h2 className="mr-auto text-lg font-semibold">Apps</h2>
        <div className="relative">
          <Search className="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-muted" aria-hidden />
          <input
            type="search"
            aria-label="Buscar apps"
            placeholder="Buscar apps"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            className={cx(inputCls, 'w-48 pl-9 sm:w-64')}
          />
        </div>
      </div>

      {categories.length > 2 && (
        <div className="mb-4 flex flex-wrap gap-2" role="tablist" aria-label="Categorias">
          {categories.map((c) => (
            <button
              key={c}
              role="tab"
              aria-selected={c === category}
              onClick={() => setCategory(c)}
              className={cx('rounded-full px-3 py-1 text-sm transition', c === category ? 'bg-accent text-accent-fg' : 'bg-surface-2 text-muted hover:text-fg')}
            >
              {c}
            </button>
          ))}
        </div>
      )}

      {(apps.data ?? []).length === 0 ? (
        <EmptyState icon="📦" title="Nenhum app instalado ainda">
          <Link to="/store" className="inline-flex items-center gap-1.5 font-medium text-accent hover:underline">
            <Store className="h-4 w-4" aria-hidden /> Abrir a loja de apps
          </Link>
        </EmptyState>
      ) : visible.length === 0 ? (
        <EmptyState icon="🔍" title="Nenhum app encontrado">
          Tente outro termo ou categoria.
        </EmptyState>
      ) : (
        <ul className="grid grid-cols-2 gap-3 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6">
          {visible.map((a) => (
            <li key={a.id} className="h-full">
              <AppCard app={a} busy={busyId === a.id} highlight={a.id === highlight} onAction={(act) => onAction(a, act)} />
            </li>
          ))}
        </ul>
      )}

      <Modal open={!!logsFor} onClose={() => setLogsFor(null)} title={`Logs de ${logsFor?.name ?? ''}`} wide>
        {logsFor && <LogViewer path={`/apps/${logsFor.id}/logs/stream`} />}
      </Modal>

      {removing && <RemoveAppDialog app={removing} onClose={() => setRemoving(null)} />}
      {editing && <EditAppDialog app={editing} onClose={() => setEditing(null)} />}
    </section>
  )
}

/** Removing data requires typing the app name (R04.5). */
function RemoveAppDialog({ app, onClose }: { app: App; onClose: () => void }) {
  const qc = useQueryClient()
  const toast = useToast()
  const [purge, setPurge] = useState(false)
  const [confirm, setConfirm] = useState('')
  const remove = useMutation({
    mutationFn: () => del(`/apps/${app.id}?purgeData=${purge}`),
    onSuccess: () => {
      toast.ok(`${app.name} removido.`)
      onClose()
    },
    onError: (e) => toast.error(errorMessage(e)),
    onSettled: () => void qc.invalidateQueries({ queryKey: ['apps'] }),
  })
  const canRemove = !purge || confirm === app.name

  return (
    <Modal
      open
      onClose={onClose}
      title={`Remover ${app.name}`}
      footer={
        <>
          <Button onClick={onClose}>Cancelar</Button>
          <Button variant="danger" loading={remove.isPending} disabled={!canRemove} onClick={() => remove.mutate()}>
            Remover
          </Button>
        </>
      }
    >
      <div className="space-y-4 text-sm">
        <p>Os containers do app serão removidos. {app.managed ? 'Os dados continuam no servidor, a menos que você marque a opção abaixo.' : ''}</p>
        {app.managed && (
          <label className="flex items-start gap-2">
            <input type="checkbox" className="mt-1" checked={purge} onChange={(e) => setPurge(e.target.checked)} />
            <span>
              Apagar também os <strong>dados do app</strong> (configurações, bancos, etc.). Isso não pode ser desfeito.
            </span>
          </label>
        )}
        {purge && (
          <Field label={`Digite "${app.name}" para confirmar`}>
            <input className={inputCls} value={confirm} onChange={(e) => setConfirm(e.target.value)} autoComplete="off" />
          </Field>
        )}
      </div>
    </Modal>
  )
}
