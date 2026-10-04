import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ExternalLink, HardDrive, Plus, Trash2 } from 'lucide-react'
import { del, get, patch } from '../lib/api'
import { formatBytes } from '../lib/format'
import type { Disk, Share } from '../lib/types'
import { useOmvAdminUrl } from '../lib/useOmvUrl'
import { ShareWizard } from '../components/ShareWizard'
import { useToast } from '../components/Toast'
import { Badge, Button, Card, EmptyState, ErrorBox, Modal, PageHeader, ProgressBar, Spinner, errorMessage } from '../components/ui'

export function Storage() {
  const [creating, setCreating] = useState(false)
  const omvUrl = useOmvAdminUrl()
  const disks = useQuery({ queryKey: ['storage', 'filesystems'], queryFn: ({ signal }) => get<Disk[]>('/storage/filesystems', signal) })

  return (
    <>
      <PageHeader
        title="Armazenamento"
        subtitle="Discos e pastas compartilhadas na rede."
        actions={
          <>
            <a href={omvUrl} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1.5 rounded-xl bg-surface-2 px-3.5 py-2 text-sm font-medium hover:bg-line">
              <ExternalLink className="h-4 w-4" aria-hidden /> Configurações avançadas
            </a>
            <Button variant="primary" onClick={() => setCreating(true)} disabled={!disks.data?.some((d) => d.mntEntRef)}>
              <Plus className="h-4 w-4" aria-hidden /> Novo compartilhamento
            </Button>
          </>
        }
      />

      <section aria-label="Discos" className="mb-8">
        <h2 className="mb-3 text-lg font-semibold">Discos</h2>
        {disks.isPending ? (
          <Spinner />
        ) : disks.isError ? (
          <ErrorBox error={disks.error} onRetry={() => void disks.refetch()} />
        ) : disks.data.length === 0 ? (
          <Card>
            <EmptyState icon="💽" title="Nenhum disco montado">
              Monte um filesystem na interface original do OMV para começar.
            </EmptyState>
          </Card>
        ) : (
          <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {disks.data.map((d) => (
              <li key={d.uuid}>
                <Card className="p-4">
                  <div className="mb-3 flex items-center gap-3">
                    <HardDrive className="h-8 w-8 text-accent" aria-hidden />
                    <div className="min-w-0">
                      <p className="truncate font-medium">{d.label || d.device}</p>
                      <p className="truncate text-xs text-muted">
                        {d.device} · {d.type}
                      </p>
                    </div>
                  </div>
                  <ProgressBar value={d.percent} label={`Uso de ${d.label || d.device}`} />
                  <p className="mt-2 flex justify-between text-xs text-muted">
                    <span>
                      {formatBytes(d.used)} de {formatBytes(d.total)} ({d.percent.toFixed(0)}%)
                    </span>
                    {d.percent >= 90 && <span className="font-medium text-danger">quase cheio</span>}
                  </p>
                </Card>
              </li>
            ))}
          </ul>
        )}
      </section>

      <Shares />
      {creating && disks.data && <SharesWizard disks={disks.data} onClose={() => setCreating(false)} />}
    </>
  )
}

function SharesWizard({ disks, onClose }: { disks: Disk[]; onClose: () => void }) {
  const shares = useQuery({ queryKey: ['shares'], queryFn: ({ signal }) => get<Share[]>('/storage/shares', signal) })
  return <ShareWizard disks={disks} existing={(shares.data ?? []).map((s) => s.name)} onClose={onClose} />
}

function Shares() {
  const qc = useQueryClient()
  const toast = useToast()
  const [removing, setRemoving] = useState<Share | null>(null)
  const shares = useQuery({ queryKey: ['shares'], queryFn: ({ signal }) => get<Share[]>('/storage/shares', signal) })

  const toggle = useMutation({
    mutationFn: ({ s, body }: { s: Share; body: { smb?: boolean; smbGuest?: boolean } }) => patch<Share>(`/storage/shares/${s.uuid}`, body),
    onError: (e) => toast.error(errorMessage(e)),
    onSettled: () => void qc.invalidateQueries({ queryKey: ['shares'] }),
  })
  const remove = useMutation({
    mutationFn: (s: Share) => del(`/storage/shares/${s.uuid}`),
    onSuccess: (_d, s) => {
      toast.ok(`Compartilhamento ${s.name} removido. Os arquivos continuam no disco.`)
      setRemoving(null)
    },
    onError: (e) => toast.error(errorMessage(e)),
    onSettled: () => void qc.invalidateQueries({ queryKey: ['shares'] }),
  })

  return (
    <section aria-label="Compartilhamentos">
      <h2 className="mb-3 text-lg font-semibold">Compartilhamentos</h2>
      {shares.isPending ? (
        <Spinner />
      ) : shares.isError ? (
        <ErrorBox error={shares.error} onRetry={() => void shares.refetch()} />
      ) : shares.data.length === 0 ? (
        <Card>
          <EmptyState icon="📁" title="Nenhum compartilhamento">
            Crie um para acessar seus arquivos pela rede.
          </EmptyState>
        </Card>
      ) : (
        <Card className="overflow-x-auto">
          <table className="w-full min-w-[40rem] text-left text-sm">
            <thead className="text-xs uppercase text-muted">
              <tr>
                <th className="px-4 py-3">Nome</th>
                <th className="px-4 py-3">Caminho</th>
                <th className="px-4 py-3">Rede (SMB)</th>
                <th className="px-4 py-3 text-right">Ações</th>
              </tr>
            </thead>
            <tbody>
              {shares.data.map((s) => (
                <tr key={s.uuid} className="border-t border-line">
                  <td className="px-4 py-3">
                    <p className="font-medium">{s.name}</p>
                    {s.comment && <p className="text-xs text-muted">{s.comment}</p>}
                  </td>
                  <td className="max-w-[16rem] truncate px-4 py-3 font-mono text-xs text-muted" title={s.path}>{s.path || '—'}</td>
                  <td className="px-4 py-3">
                    <div className="flex flex-wrap items-center gap-2">
                      <label className="flex items-center gap-1.5">
                        <input type="checkbox" checked={s.smb} disabled={toggle.isPending} aria-label={`SMB de ${s.name}`} onChange={(e) => toggle.mutate({ s, body: { smb: e.target.checked } })} />
                        {s.smb ? <Badge tone="ok">ativo</Badge> : <Badge>desligado</Badge>}
                      </label>
                      {s.smb && (
                        <label className="flex items-center gap-1.5 text-xs text-muted">
                          <input type="checkbox" checked={s.smbGuest} disabled={toggle.isPending} aria-label={`Convidado em ${s.name}`} onChange={(e) => toggle.mutate({ s, body: { smbGuest: e.target.checked } })} />
                          convidado
                        </label>
                      )}
                      {s.nfs && <Badge>NFS</Badge>}
                    </div>
                  </td>
                  <td className="px-4 py-3 text-right">
                    <button type="button" aria-label={`Remover ${s.name}`} onClick={() => setRemoving(s)} className="rounded-lg p-2 text-muted hover:bg-surface-2 hover:text-danger">
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </Card>
      )}

      {removing && (
        <Modal
          open
          onClose={() => setRemoving(null)}
          title={`Remover ${removing.name}`}
          footer={
            <>
              <Button onClick={() => setRemoving(null)}>Cancelar</Button>
              <Button variant="danger" loading={remove.isPending} onClick={() => remove.mutate(removing)}>
                Remover compartilhamento
              </Button>
            </>
          }
        >
          <p className="text-sm">
            O compartilhamento deixará de aparecer na rede. <strong>Os arquivos continuam no disco</strong> — nada é apagado.
          </p>
        </Modal>
      )}
    </section>
  )
}
