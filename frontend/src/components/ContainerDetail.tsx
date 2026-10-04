import { useQuery } from '@tanstack/react-query'
import { get } from '../lib/api'
import { formatBytes } from '../lib/format'
import type { ContainerDetail as Detail } from '../lib/types'
import { LogViewer } from './LogViewer'
import { Badge, ErrorBox, Modal, Spinner } from './ui'

type ContainerStats = { cpuPercent: number; memoryUsed: number; memoryLimit: number; netRx: number; netTx: number }

export function ContainerDetailModal({ name, onClose }: { name: string; onClose: () => void }) {
  const detail = useQuery({ queryKey: ['container', name], queryFn: ({ signal }) => get<Detail>(`/containers/${name}`, signal) })
  const running = detail.data?.state === 'running'
  const stats = useQuery({
    queryKey: ['container-stats', name],
    queryFn: ({ signal }) => get<ContainerStats>(`/containers/${name}/stats`, signal),
    enabled: running,
    refetchInterval: 5_000,
  })

  return (
    <Modal open onClose={onClose} title={`Container ${name}`} wide>
      {detail.isPending ? (
        <Spinner />
      ) : detail.isError ? (
        <ErrorBox error={detail.error} onRetry={() => void detail.refetch()} />
      ) : (
        <div className="space-y-5 text-sm">
          <dl className="grid gap-x-6 gap-y-2 sm:grid-cols-2">
            <Row k="Imagem" v={detail.data.image} />
            <Row k="Estado" v={<Badge tone={running ? 'ok' : 'neutral'}>{detail.data.state}</Badge>} />
            <Row k="Reinício" v={detail.data.restartPolicy || 'no'} />
            <Row k="Redes" v={detail.data.networks.join(', ') || '—'} />
            {stats.data && <Row k="CPU / Memória" v={`${stats.data.cpuPercent.toFixed(1)}% · ${formatBytes(stats.data.memoryUsed)} / ${formatBytes(stats.data.memoryLimit)}`} />}
            {detail.data.command && <Row k="Comando" v={<code className="text-xs">{detail.data.command.join(' ')}</code>} />}
          </dl>

          <Section title="Variáveis de ambiente" empty={detail.data.env.length === 0}>
            <table className="w-full text-left">
              <tbody>
                {detail.data.env.map((e) => (
                  <tr key={e.key} className="border-t border-line">
                    <td className="py-1 pr-4 font-mono text-xs">{e.key}</td>
                    <td className="py-1 font-mono text-xs text-muted">{e.value}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </Section>

          <Section title="Volumes" empty={detail.data.mounts.length === 0}>
            <ul className="space-y-1 font-mono text-xs">
              {detail.data.mounts.map((m) => (
                <li key={m.destination}>
                  {m.source} → {m.destination} {m.readOnly && <Badge>somente leitura</Badge>}
                </li>
              ))}
            </ul>
          </Section>

          <div>
            <h3 className="mb-2 font-medium">Logs</h3>
            <LogViewer path={`/containers/${name}/logs/stream`} />
          </div>
        </div>
      )}
    </Modal>
  )
}

function Row({ k, v }: { k: string; v: React.ReactNode }) {
  return (
    <div className="flex gap-2">
      <dt className="w-28 shrink-0 text-muted">{k}</dt>
      <dd className="min-w-0 break-words">{v}</dd>
    </div>
  )
}

function Section({ title, empty, children }: { title: string; empty: boolean; children: React.ReactNode }) {
  return (
    <div>
      <h3 className="mb-1 font-medium">{title}</h3>
      {empty ? <p className="text-muted">Nenhum.</p> : children}
    </div>
  )
}
