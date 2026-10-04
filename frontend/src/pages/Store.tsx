import { useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Plus, Search } from 'lucide-react'
import { ApiError, get, post } from '../lib/api'
import type { StoreCard } from '../lib/types'
import { validatePort } from '../lib/install'
import { AppIcon } from '../components/AppIcon'
import { InstallProgress } from '../components/InstallProgress'
import { Badge, Button, Card, EmptyState, ErrorBox, Field, Modal, PageHeader, Spinner, cx, errorMessage, inputCls } from '../components/ui'

const ALL = 'Todos'

export function filterStore(cards: StoreCard[], query: string, category: string): StoreCard[] {
  const q = query.trim().toLowerCase()
  return cards.filter(
    (c) => (category === ALL || c.category === category) && (!q || `${c.title} ${c.tagline} ${c.id}`.toLowerCase().includes(q)),
  )
}

export function Store() {
  const [query, setQuery] = useState('')
  const [category, setCategory] = useState(ALL)
  const [custom, setCustom] = useState(false)
  const cards = useQuery({ queryKey: ['store'], queryFn: ({ signal }) => get<StoreCard[]>('/store/apps', signal) })

  const categories = useMemo(() => [ALL, ...Array.from(new Set((cards.data ?? []).map((c) => c.category))).sort()], [cards.data])
  const visible = useMemo(() => filterStore(cards.data ?? [], query, category), [cards.data, query, category])

  return (
    <>
      <PageHeader
        title="Loja de apps"
        subtitle="Instale aplicativos com um clique. Cada app roda em um container Docker."
        actions={
          <Button onClick={() => setCustom(true)}>
            <Plus className="h-4 w-4" aria-hidden /> App personalizado
          </Button>
        }
      />

      <div className="mb-4 flex flex-wrap items-center gap-3">
        <div className="relative">
          <Search className="pointer-events-none absolute left-3 top-2.5 h-4 w-4 text-muted" aria-hidden />
          <input type="search" aria-label="Buscar na loja" placeholder="Buscar na loja" value={query} onChange={(e) => setQuery(e.target.value)} className={cx(inputCls, 'w-64 pl-9')} />
        </div>
        <div className="flex flex-wrap gap-2" role="tablist" aria-label="Categorias">
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
      </div>

      {cards.isPending ? (
        <Spinner />
      ) : cards.isError ? (
        <ErrorBox error={cards.error} onRetry={() => void cards.refetch()} />
      ) : visible.length === 0 ? (
        <EmptyState icon="🔍" title="Nada encontrado">
          Tente outro termo ou categoria.
        </EmptyState>
      ) : (
        <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {visible.map((c) => (
            <li key={c.id}>
              <Link to={`/store/${c.id}`} className="block rounded-2xl focus-visible:outline-offset-4">
                <Card className="flex items-center gap-4 p-4 transition hover:-translate-y-0.5">
                  <AppIcon icon={c.icon} name={c.title} />
                  <div className="min-w-0 flex-1">
                    <p className="flex items-center gap-2 font-medium">
                      <span className="truncate">{c.title}</span>
                      {c.installed && <Badge tone="ok">Instalado</Badge>}
                    </p>
                    <p className="line-clamp-2 text-sm text-muted">{c.tagline}</p>
                  </div>
                </Card>
              </Link>
            </li>
          ))}
        </ul>
      )}

      {custom && <CustomInstallDialog onClose={() => setCustom(false)} />}
    </>
  )
}

function CustomInstallDialog({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [name, setName] = useState('')
  const [compose, setCompose] = useState('services:\n  app:\n    image: \n    ports:\n      - "8080:80"\n')
  const [mainService, setMainService] = useState('')
  const [port, setPort] = useState('')
  const [allowUnsafe, setAllowUnsafe] = useState(false)
  const [needsUnsafe, setNeedsUnsafe] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [jobId, setJobId] = useState<string | null>(null)

  const portError = validatePort(port)
  const valid = name.trim() && compose.trim() && !portError

  async function submit() {
    setError(null)
    setBusy(true)
    try {
      const r = await post<{ jobId: string }>('/store/custom', { name: name.trim(), compose, mainService: mainService.trim(), port: port ? Number(port) : 0, allowUnsafe })
      setJobId(r.jobId)
    } catch (e) {
      setError(errorMessage(e))
      if (e instanceof ApiError && e.message.includes('allowUnsafe')) setNeedsUnsafe(true)
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      open
      wide
      onClose={onClose}
      title="App personalizado (docker compose)"
      footer={
        jobId ? (
          <Button onClick={onClose}>Fechar</Button>
        ) : (
          <>
            <Button onClick={onClose}>Cancelar</Button>
            <Button variant="primary" loading={busy} disabled={!valid} onClick={() => void submit()}>
              Instalar
            </Button>
          </>
        )
      }
    >
      {jobId ? (
        <InstallProgress
          appId={jobId}
          onDone={() => {
            void qc.invalidateQueries({ queryKey: ['apps'] })
            void qc.invalidateQueries({ queryKey: ['store'] })
            setTimeout(() => navigate(`/?highlight=${jobId}`), 1200)
          }}
        />
      ) : (
        <div className="space-y-4">
          <Field label="Nome do app">
            <input className={inputCls} value={name} onChange={(e) => setName(e.target.value)} placeholder="Meu app" />
          </Field>
          <Field label="docker-compose.yml" hint="Use ${APPDATA} para guardar dados em uma pasta gerenciada pelo OMV Home.">
            <textarea className={cx(inputCls, 'h-48 font-mono text-xs')} value={compose} onChange={(e) => setCompose(e.target.value)} spellCheck={false} />
          </Field>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Serviço principal" hint="Obrigatório se houver mais de um serviço.">
              <input className={inputCls} value={mainService} onChange={(e) => setMainService(e.target.value)} placeholder="app" />
            </Field>
            <Field label="Porta web do host" hint="Usada pelo botão “Abrir”." error={portError}>
              <input className={inputCls} inputMode="numeric" value={port} onChange={(e) => setPort(e.target.value)} placeholder="8080" />
            </Field>
          </div>
          {needsUnsafe && (
            <label className="flex items-start gap-2 rounded-xl border border-warn/40 bg-warn/10 p-3 text-sm">
              <input type="checkbox" className="mt-1" checked={allowUnsafe} onChange={(e) => setAllowUnsafe(e.target.checked)} />
              <span>
                Permitir configurações inseguras (privileged, rede host, acesso ao Docker). Isso dá ao container acesso total ao servidor.
              </span>
            </label>
          )}
          {error && (
            <p role="alert" className="rounded-xl border border-danger/40 bg-danger/10 p-2.5 text-sm text-danger">
              {error}
            </p>
          )}
        </div>
      )}
    </Modal>
  )
}
