import { useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft } from 'lucide-react'
import { get, post } from '../lib/api'
import { initialValues, validateField, validatePort } from '../lib/install'
import type { DockerStatus, EnvField, StoreDetail } from '../lib/types'
import { AppIcon } from '../components/AppIcon'
import { InstallProgress } from '../components/InstallProgress'
import { Badge, Button, Card, ErrorBox, Field, Spinner, errorMessage, inputCls } from '../components/ui'

export function StoreApp() {
  const { id = '' } = useParams()
  const detail = useQuery({ queryKey: ['store', id], queryFn: ({ signal }) => get<StoreDetail>(`/store/apps/${id}`, signal) })
  const status = useQuery({ queryKey: ['docker-status'], queryFn: ({ signal }) => get<DockerStatus>('/docker/status', signal) })

  return (
    <>
      <Link to="/store" className="mb-4 inline-flex items-center gap-1.5 text-sm text-muted hover:text-fg">
        <ArrowLeft className="h-4 w-4" aria-hidden /> Voltar para a loja
      </Link>
      {detail.isPending ? (
        <Spinner />
      ) : detail.isError ? (
        <ErrorBox error={detail.error} onRetry={() => void detail.refetch()} />
      ) : (
        <Detail data={detail.data} composeOk={status.data?.composeAvailable ?? true} dockerOk={status.data?.available ?? true} />
      )}
    </>
  )
}

function Detail({ data, composeOk, dockerOk }: { data: StoreDetail; composeOk: boolean; dockerOk: boolean }) {
  const { meta } = data
  const fields = meta.env ?? []
  const qc = useQueryClient()
  const navigate = useNavigate()
  const [values, setValues] = useState(() => initialValues(fields))
  const [port, setPort] = useState(meta.port ? String(meta.port) : '')
  const [touched, setTouched] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [installing, setInstalling] = useState(false)
  const [allowUnsafe, setAllowUnsafe] = useState(false)
  const unsafe = data.unsafe ?? []
  const warnings = data.warnings ?? []

  const errors: Record<string, string | undefined> = Object.fromEntries(fields.map((f) => [f.key, validateField(f, values[f.key] ?? '')]))
  const portError = validatePort(port)
  const hasErrors = Object.values(errors).some(Boolean) || !!portError
  const canInstall = composeOk && dockerOk && !data.installed && !data.unsupported && (unsafe.length === 0 || allowUnsafe)

  async function install() {
    setTouched(true)
    if (hasErrors) return
    setError(null)
    setSubmitting(true)
    try {
      await post(`/store/apps/${meta.id}/install`, { env: values, port: port ? Number(port) : 0, ...(allowUnsafe ? { allowUnsafe: true } : {}) })
      void qc.invalidateQueries({ queryKey: ['installs'] })
      setInstalling(true)
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_22rem]">
      <div className="min-w-0">
        <div className="mb-4 flex items-center gap-4">
          <AppIcon icon={meta.icon} name={meta.title} className="h-20 w-20" />
          <div>
            <h1 className="flex items-center gap-2 text-2xl font-semibold">
              {meta.title}
              {data.installed && <Badge tone="ok">Instalado</Badge>}
            </h1>
            <p className="text-muted">{meta.tagline}</p>
            <div className="flex flex-wrap gap-2">
              <Badge>{meta.category}</Badge>
              {data.source && <Badge>Fonte: {data.sourceName || data.source}</Badge>}
            </div>
          </div>
        </div>
        <p className="mb-6 whitespace-pre-line break-words leading-relaxed">{meta.description}</p>
        <details className="rounded-xl border border-line bg-surface p-3 text-sm">
          <summary className="cursor-pointer font-medium">Ver docker-compose</summary>
          <pre className="mt-3 max-w-full overflow-auto font-mono text-xs">{data.compose}</pre>
        </details>
      </div>

      <Card className="h-fit p-5">
        {installing ? (
          <InstallProgress
            appId={meta.id}
            onDone={() => {
              void qc.invalidateQueries({ queryKey: ['apps'] })
              void qc.invalidateQueries({ queryKey: ['store'] })
              setTimeout(() => navigate(`/?highlight=${meta.id}`), 1200)
            }}
          />
        ) : (
          <form
            className="space-y-4"
            noValidate
            onSubmit={(e) => {
              e.preventDefault()
              void install()
            }}
          >
            <h2 className="text-lg font-semibold">Instalar</h2>
            {!dockerOk && <p role="alert" className="rounded-xl border border-danger/40 bg-danger/10 p-2.5 text-sm text-danger">O Docker não está disponível no servidor.</p>}
            {dockerOk && !composeOk && <p role="alert" className="rounded-xl border border-danger/40 bg-danger/10 p-2.5 text-sm text-danger">O docker compose não está instalado; a instalação de apps está desativada.</p>}
            {data.installed && <p className="text-sm text-muted">Este app já está instalado. Gerencie-o na tela inicial.</p>}
            {data.unsupported && (
              <p role="alert" className="rounded-xl border border-warn/40 bg-warn/10 p-2.5 text-sm">
                Este app não pode ser instalado aqui: {data.unsupported}.
              </p>
            )}
            {data.source && !data.unsupported && (
              <p className="rounded-xl border border-line bg-surface-2 p-2.5 text-xs text-muted">
                App de uma fonte externa, sem verificação. Revise o docker-compose ao lado antes de instalar.
              </p>
            )}
            {warnings.length > 0 && (
              <ul className="list-disc space-y-1 rounded-xl border border-warn/40 bg-warn/10 p-2.5 pl-6 text-xs" aria-label="Avisos">
                {warnings.map((w) => (
                  <li key={w}>{w}</li>
                ))}
              </ul>
            )}
            {unsafe.length > 0 && !data.unsupported && (
              <label className="flex items-start gap-2 rounded-xl border border-danger/40 bg-danger/10 p-3 text-sm">
                <input type="checkbox" className="mt-1" checked={allowUnsafe} onChange={(e) => setAllowUnsafe(e.target.checked)} />
                <span>
                  Permitir configurações inseguras ({unsafe.join("; ")}). Isso dá ao container acesso amplo ao servidor.
                </span>
              </label>
            )}

            {fields.map((f) => (
              <FieldInput key={f.key} field={f} value={values[f.key] ?? ''} error={touched ? errors[f.key] : undefined} onChange={(v) => setValues((s) => ({ ...s, [f.key]: v }))} />
            ))}

            {meta.port > 0 && (
              <Field label="Porta de acesso" hint={`Padrão: ${meta.port}`} error={touched ? portError : undefined}>
                <input className={inputCls} inputMode="numeric" value={port} onChange={(e) => setPort(e.target.value)} />
              </Field>
            )}

            {error && (
              <p role="alert" className="rounded-xl border border-danger/40 bg-danger/10 p-2.5 text-sm text-danger">
                {error}
              </p>
            )}
            <Button type="submit" variant="primary" className="w-full" loading={submitting} disabled={!canInstall}>
              Instalar {meta.title}
            </Button>
          </form>
        )}
      </Card>
    </div>
  )
}

function FieldInput({ field, value, error, onChange }: { field: EnvField; value: string; error?: string; onChange: (v: string) => void }) {
  const label = field.required ? field.label : `${field.label} (opcional)`
  const hint = field.type === 'path' ? 'Pasta do servidor (caminho absoluto). Será criada se não existir.' : undefined
  let control
  switch (field.type) {
    case 'select':
      control = (
        <select className={inputCls} value={value} onChange={(e) => onChange(e.target.value)}>
          {(field.options ?? []).map((o) => (
            <option key={o}>{o}</option>
          ))}
        </select>
      )
      break
    case 'bool':
      control = (
        <select className={inputCls} value={value || 'false'} onChange={(e) => onChange(e.target.value)}>
          <option value="true">Sim</option>
          <option value="false">Não</option>
        </select>
      )
      break
    default:
      control = (
        <input
          className={inputCls}
          type={field.type === 'password' ? 'password' : 'text'}
          inputMode={field.type === 'port' ? 'numeric' : undefined}
          autoComplete={field.type === 'password' ? 'new-password' : 'off'}
          value={value}
          onChange={(e) => onChange(e.target.value)}
        />
      )
  }
  return (
    <Field label={label} hint={hint} error={error}>
      {control}
    </Field>
  )
}
