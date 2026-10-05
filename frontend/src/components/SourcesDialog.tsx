import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { RefreshCw, Trash2 } from 'lucide-react'
import { del, get, post } from '../lib/api'
import type { StoreSource } from '../lib/types'
import { useToast } from './Toast'
import { Badge, Button, ErrorBox, Field, Modal, Spinner, errorMessage, inputCls } from './ui'

export const FORMAT_LABEL: Record<StoreSource['format'], string> = { umbrel: 'Umbrel', casaos: 'ZimaOS / CasaOS' }

/** Manages custom app sources: list, add by URL, refresh and remove (R09.9). */
export function SourcesDialog({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient()
  const toast = useToast()
  const [url, setUrl] = useState('')
  const [name, setName] = useState('')
  const [confirm, setConfirm] = useState<string | null>(null)
  const sources = useQuery({ queryKey: ['store-sources'], queryFn: ({ signal }) => get<StoreSource[]>('/store/sources', signal) })

  const changed = () => {
    void qc.invalidateQueries({ queryKey: ['store-sources'] })
    void qc.invalidateQueries({ queryKey: ['store'] })
  }
  const add = useMutation({
    mutationFn: () => post<StoreSource>('/store/sources', { url: url.trim(), name: name.trim() }),
    onSuccess: (s) => {
      toast.ok(`Fonte ${s.name} adicionada (${s.apps} apps).`)
      setUrl('')
      setName('')
      changed()
    },
  })
  const refresh = useMutation({
    mutationFn: (id: string) => post<StoreSource>(`/store/sources/${id}/refresh`),
    onSuccess: (s) => {
      toast.ok(`Fonte ${s.name} atualizada.`)
      changed()
    },
    onError: (e) => {
      toast.error(errorMessage(e))
      changed()
    },
  })
  const remove = useMutation({
    mutationFn: (id: string) => del(`/store/sources/${id}`),
    onSuccess: () => {
      setConfirm(null)
      toast.ok('Fonte removida. Apps já instalados continuam funcionando.')
      changed()
    },
    onError: (e) => toast.error(errorMessage(e)),
  })

  return (
    <Modal open wide onClose={onClose} title="Fontes de apps" footer={<Button onClick={onClose}>Fechar</Button>}>
      <p className="mb-4 text-sm text-muted">
        Adicione lojas de apps de terceiros no formato do ZimaOS/CasaOS ou do Umbrel. Use o endereço de um repositório do GitHub ou de um arquivo
        .zip. Apps de fontes externas não são verificados: revise o compose antes de instalar.
      </p>

      {sources.isPending ? (
        <Spinner />
      ) : sources.isError ? (
        <ErrorBox error={sources.error} onRetry={() => void sources.refetch()} />
      ) : sources.data.length === 0 ? (
        <p className="mb-4 rounded-xl border border-line bg-surface-2 p-3 text-sm text-muted">Nenhuma fonte adicionada ainda.</p>
      ) : (
        <ul className="mb-5 space-y-2" aria-label="Fontes adicionadas">
          {sources.data.map((s) => (
            <li key={s.id} className="rounded-xl border border-line p-3 text-sm">
              <div className="flex items-start gap-3">
                <div className="min-w-0 flex-1">
                  <p className="flex flex-wrap items-center gap-2 font-medium">
                    {s.name} <Badge>{FORMAT_LABEL[s.format] ?? s.format}</Badge>
                  </p>
                  <p className="truncate text-xs text-muted" title={s.url}>
                    {s.url}
                  </p>
                  <p className="text-xs text-muted">
                    {s.apps} apps · atualizada em {new Date(s.updatedAt).toLocaleString()}
                  </p>
                  {s.error && (
                    <p role="alert" className="mt-1 text-xs text-danger">
                      {s.error}
                    </p>
                  )}
                </div>
                {confirm === s.id ? (
                  <div className="flex shrink-0 gap-2">
                    <Button onClick={() => setConfirm(null)}>Cancelar</Button>
                    <Button variant="danger" loading={remove.isPending} onClick={() => remove.mutate(s.id)}>
                      Remover {s.name}
                    </Button>
                  </div>
                ) : (
                  <div className="flex shrink-0 gap-1">
                    <Button aria-label={`Atualizar ${s.name}`} loading={refresh.isPending && refresh.variables === s.id} onClick={() => refresh.mutate(s.id)}>
                      <RefreshCw className="h-4 w-4" aria-hidden />
                    </Button>
                    <Button aria-label={`Remover ${s.name}`} onClick={() => setConfirm(s.id)}>
                      <Trash2 className="h-4 w-4" aria-hidden />
                    </Button>
                  </div>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}

      <form
        className="space-y-3"
        onSubmit={(e) => {
          e.preventDefault()
          if (url.trim()) add.mutate()
        }}
      >
        <h3 className="text-sm font-semibold">Adicionar fonte</h3>
        <Field label="Endereço da fonte" hint="Ex.: https://github.com/WisdomSky/LinuxServer-AppStore (ZimaOS) ou https://github.com/dennysubke/dennys-umbrel-app-store (Umbrel)">
          <input className={inputCls} type="url" inputMode="url" value={url} onChange={(e) => setUrl(e.target.value)} placeholder="https://github.com/dono/repositorio" />
        </Field>
        <Field label="Nome (opcional)">
          <input className={inputCls} value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        {add.isError && (
          <p role="alert" className="rounded-xl border border-danger/40 bg-danger/10 p-2.5 text-sm text-danger">
            {errorMessage(add.error)}
          </p>
        )}
        <Button type="submit" variant="primary" loading={add.isPending} disabled={!url.trim()}>
          Adicionar fonte
        </Button>
        {add.isPending && <p className="text-xs text-muted">Baixando e convertendo os apps… isso pode levar alguns segundos.</p>}
      </form>
    </Modal>
  )
}
