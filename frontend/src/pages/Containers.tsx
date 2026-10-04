import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Download, FileText, Play, Plus, RotateCw, Square, Trash2 } from 'lucide-react'
import { del, get, post } from '../lib/api'
import { formatBytes } from '../lib/format'
import { streamPost } from '../lib/sse'
import type { Container, DockerImage, DockerStatus } from '../lib/types'
import { ContainerDetailModal } from '../components/ContainerDetail'
import { RunDialog } from '../components/RunDialog'
import { useToast } from '../components/Toast'
import { Badge, Button, Card, EmptyState, ErrorBox, Field, Modal, PageHeader, Spinner, cx, errorMessage, inputCls } from '../components/ui'

export function Containers() {
  const [tab, setTab] = useState<'containers' | 'images'>('containers')
  const [running, setRunning] = useState(false)
  const status = useQuery({ queryKey: ['docker-status'], queryFn: ({ signal }) => get<DockerStatus>('/docker/status', signal) })

  return (
    <>
      <PageHeader
        title="Containers"
        subtitle={status.data?.available ? `Docker ${status.data.version}` : 'Gerencie containers e imagens Docker.'}
        actions={
          <Button variant="primary" onClick={() => setRunning(true)} disabled={status.data?.available === false}>
            <Plus className="h-4 w-4" aria-hidden /> Novo container
          </Button>
        }
      />
      {status.data && !status.data.available && <div className="mb-4"><ErrorBox error={new Error('O Docker não está disponível no servidor.')} onRetry={() => void status.refetch()} /></div>}

      <div className="mb-4 flex gap-2" role="tablist">
        {(['containers', 'images'] as const).map((t) => (
          <button
            key={t}
            role="tab"
            aria-selected={tab === t}
            onClick={() => setTab(t)}
            className={cx('rounded-full px-3 py-1 text-sm', tab === t ? 'bg-accent text-accent-fg' : 'bg-surface-2 text-muted hover:text-fg')}
          >
            {t === 'containers' ? 'Containers' : 'Imagens'}
          </button>
        ))}
      </div>

      {tab === 'containers' ? <ContainerList /> : <ImageList />}
      {running && <RunDialog onClose={() => setRunning(false)} />}
    </>
  )
}

const tone = (s: string) => (s === 'running' ? 'ok' : s === 'exited' || s === 'created' ? 'neutral' : 'warn')

function ContainerList() {
  const qc = useQueryClient()
  const toast = useToast()
  const [detail, setDetail] = useState<string | null>(null)
  const [removing, setRemoving] = useState<Container | null>(null)
  const list = useQuery({ queryKey: ['containers'], queryFn: ({ signal }) => get<Container[]>('/containers', signal), refetchInterval: 8_000 })

  const act = useMutation({
    mutationFn: ({ c, action }: { c: Container; action: 'start' | 'stop' | 'restart' }) => post(`/containers/${c.name}/${action}`),
    onError: (e, { c }) => toast.error(`${c.name}: ${errorMessage(e)}`),
    onSettled: () => void qc.invalidateQueries({ queryKey: ['containers'] }),
  })

  if (list.isPending) return <Spinner />
  if (list.isError) return <ErrorBox error={list.error} onRetry={() => void list.refetch()} />
  if (list.data.length === 0) return <Card><EmptyState icon="📦" title="Nenhum container">Crie um com “Novo container” ou instale um app pela loja.</EmptyState></Card>

  return (
    <>
      <Card className="overflow-x-auto">
        <table className="w-full min-w-[40rem] text-left text-sm">
          <thead className="text-xs uppercase text-muted">
            <tr>
              <th className="px-4 py-3">Nome</th>
              <th className="px-4 py-3">Imagem</th>
              <th className="px-4 py-3">Estado</th>
              <th className="px-4 py-3">Portas</th>
              <th className="px-4 py-3 text-right">Ações</th>
            </tr>
          </thead>
          <tbody>
            {list.data.map((c) => {
              const isRunning = c.state === 'running'
              return (
                <tr key={c.id} className="border-t border-line">
                  <td className="px-4 py-3 font-medium">
                    <button className="hover:underline" onClick={() => setDetail(c.name)}>
                      {c.name}
                    </button>
                    {c.appId && <span className="ml-2"><Badge>app</Badge></span>}
                  </td>
                  <td className="max-w-[14rem] truncate px-4 py-3 text-muted" title={c.image}>{c.image}</td>
                  <td className="px-4 py-3">
                    <Badge tone={tone(c.state)}>{c.state}</Badge>
                    <div className="text-xs text-muted">{c.status}</div>
                  </td>
                  <td className="px-4 py-3 text-xs text-muted">{c.ports.length ? c.ports.map((p) => `${p.public}→${p.private}/${p.protocol}`).join(', ') : '—'}</td>
                  <td className="px-4 py-3">
                    <div className="flex justify-end gap-1">
                      {isRunning ? (
                        <IconBtn label={`Parar ${c.name}`} onClick={() => act.mutate({ c, action: 'stop' })}><Square className="h-4 w-4" /></IconBtn>
                      ) : (
                        <IconBtn label={`Iniciar ${c.name}`} onClick={() => act.mutate({ c, action: 'start' })}><Play className="h-4 w-4" /></IconBtn>
                      )}
                      <IconBtn label={`Reiniciar ${c.name}`} onClick={() => act.mutate({ c, action: 'restart' })}><RotateCw className="h-4 w-4" /></IconBtn>
                      <IconBtn label={`Detalhes e logs de ${c.name}`} onClick={() => setDetail(c.name)}><FileText className="h-4 w-4" /></IconBtn>
                      <IconBtn
                        label={c.appId ? `${c.name} pertence ao app ${c.appId}; remova pelo app` : `Remover ${c.name}`}
                        disabled={!!c.appId}
                        danger
                        onClick={() => setRemoving(c)}
                      >
                        <Trash2 className="h-4 w-4" />
                      </IconBtn>
                    </div>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </Card>
      {detail && <ContainerDetailModal name={detail} onClose={() => setDetail(null)} />}
      {removing && <RemoveContainerDialog c={removing} onClose={() => setRemoving(null)} />}
    </>
  )
}

function IconBtn({ label, onClick, disabled, danger, children }: { label: string; onClick: () => void; disabled?: boolean; danger?: boolean; children: React.ReactNode }) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      disabled={disabled}
      onClick={onClick}
      className={cx('rounded-lg p-2 text-muted hover:bg-surface-2 disabled:cursor-not-allowed disabled:opacity-40', danger ? 'hover:text-danger' : 'hover:text-fg')}
    >
      {children}
    </button>
  )
}

function RemoveContainerDialog({ c, onClose }: { c: Container; onClose: () => void }) {
  const qc = useQueryClient()
  const toast = useToast()
  const [volumes, setVolumes] = useState(false)
  const isRunning = c.state === 'running'
  const remove = useMutation({
    mutationFn: () => del(`/containers/${c.name}?force=${isRunning}&volumes=${volumes}`),
    onSuccess: () => {
      toast.ok(`${c.name} removido.`)
      onClose()
    },
    onError: (e) => toast.error(errorMessage(e)),
    onSettled: () => void qc.invalidateQueries({ queryKey: ['containers'] }),
  })
  return (
    <Modal
      open
      onClose={onClose}
      title={`Remover ${c.name}`}
      footer={
        <>
          <Button onClick={onClose}>Cancelar</Button>
          <Button variant="danger" loading={remove.isPending} onClick={() => remove.mutate()}>
            Remover
          </Button>
        </>
      }
    >
      <div className="space-y-3 text-sm">
        <p>{isRunning ? 'O container está em execução e será parado antes de ser removido.' : 'O container será removido.'}</p>
        <label className="flex items-start gap-2">
          <input type="checkbox" className="mt-1" checked={volumes} onChange={(e) => setVolumes(e.target.checked)} />
          <span>Remover também os volumes anônimos. Pastas do servidor mapeadas (bind) nunca são apagadas.</span>
        </label>
      </div>
    </Modal>
  )
}

function ImageList() {
  const qc = useQueryClient()
  const toast = useToast()
  const [ref, setRef] = useState('')
  const [pulling, setPulling] = useState(false)
  const [progress, setProgress] = useState<string[]>([])
  const [error, setError] = useState<string | null>(null)
  const list = useQuery({ queryKey: ['images'], queryFn: ({ signal }) => get<DockerImage[]>('/images', signal) })

  const remove = useMutation({
    mutationFn: (img: DockerImage) => del(`/images/${img.id}`),
    onSuccess: () => toast.ok('Imagem removida.'),
    onError: (e) => toast.error(errorMessage(e)),
    onSettled: () => void qc.invalidateQueries({ queryKey: ['images'] }),
  })

  async function pull() {
    setPulling(true)
    setError(null)
    setProgress([])
    try {
      await streamPost<{ status?: string; id?: string; progress?: string; message?: string }>('/images/pull', { ref: ref.trim() }, (event, data) => {
        if (event === 'error') throw new Error(data.message ?? 'Falha ao baixar a imagem')
        if (event === 'progress') setProgress((p) => [...p.slice(-50), [data.id, data.status, data.progress].filter(Boolean).join(' ')])
        if (event === 'done') toast.ok(`Imagem ${ref.trim()} baixada.`)
      })
      setRef('')
    } catch (e) {
      setError(errorMessage(e))
    } finally {
      setPulling(false)
      void qc.invalidateQueries({ queryKey: ['images'] })
    }
  }

  return (
    <div className="space-y-4">
      <Card className="p-4">
        <form
          className="flex flex-wrap items-end gap-3"
          onSubmit={(e) => {
            e.preventDefault()
            if (ref.trim()) void pull()
          }}
        >
          <div className="min-w-[14rem] flex-1">
            <Field label="Baixar imagem">
              <input className={inputCls} value={ref} onChange={(e) => setRef(e.target.value)} placeholder="nginx:alpine" disabled={pulling} />
            </Field>
          </div>
          <Button type="submit" variant="primary" loading={pulling} disabled={!ref.trim()}>
            <Download className="h-4 w-4" aria-hidden /> Baixar
          </Button>
        </form>
        {progress.length > 0 && <pre className="mt-3 max-h-40 overflow-auto rounded-xl bg-black/80 p-3 font-mono text-xs text-green-200">{progress.join('\n')}</pre>}
        {error && <p role="alert" className="mt-3 text-sm text-danger">{error}</p>}
      </Card>

      {list.isPending ? (
        <Spinner />
      ) : list.isError ? (
        <ErrorBox error={list.error} onRetry={() => void list.refetch()} />
      ) : list.data.length === 0 ? (
        <Card><EmptyState icon="🧱" title="Nenhuma imagem" /></Card>
      ) : (
        <Card className="overflow-x-auto">
          <table className="w-full min-w-[32rem] text-left text-sm">
            <thead className="text-xs uppercase text-muted">
              <tr>
                <th className="px-4 py-3">Imagem</th>
                <th className="px-4 py-3">Tamanho</th>
                <th className="px-4 py-3">Uso</th>
                <th className="px-4 py-3 text-right">Ações</th>
              </tr>
            </thead>
            <tbody>
              {list.data.map((img) => {
                const label = img.tags[0] ?? img.id
                return (
                  <tr key={img.id} className="border-t border-line">
                    <td className="px-4 py-3 font-medium">{label}</td>
                    <td className="px-4 py-3 text-muted">{formatBytes(img.size)}</td>
                    <td className="px-4 py-3">{img.inUse ? <Badge tone="ok">em uso</Badge> : <Badge>sem uso</Badge>}</td>
                    <td className="px-4 py-3 text-right">
                      <IconBtn label={img.inUse ? `${label} está em uso` : `Remover imagem ${label}`} disabled={img.inUse} danger onClick={() => remove.mutate(img)}>
                        <Trash2 className="h-4 w-4" />
                      </IconBtn>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </Card>
      )}
    </div>
  )
}
