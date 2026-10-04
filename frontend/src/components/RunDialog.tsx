import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Plus, Trash2 } from 'lucide-react'
import { post } from '../lib/api'
import { emptyForm, formToSpec, specToForm, validateForm, type RunForm } from '../lib/run'
import type { RunSpec } from '../lib/types'
import { useToast } from './Toast'
import { Button, Field, Modal, cx, errorMessage, inputCls } from './ui'

const RESTART = [
  { v: 'unless-stopped', l: 'A menos que eu pare' },
  { v: 'always', l: 'Sempre' },
  { v: 'on-failure', l: 'Em caso de falha' },
  { v: 'no', l: 'Nunca' },
]

export function RunDialog({ onClose }: { onClose: () => void }) {
  const qc = useQueryClient()
  const toast = useToast()
  const [form, setForm] = useState<RunForm>(emptyForm)
  const [tab, setTab] = useState<'form' | 'import'>('form')
  const [cmd, setCmd] = useState('')
  const [importError, setImportError] = useState<string | null>(null)
  const [touched, setTouched] = useState(false)

  const set = <K extends keyof RunForm>(k: K, v: RunForm[K]) => setForm((f) => ({ ...f, [k]: v }))
  const problem = validateForm(form)

  const importCmd = useMutation({
    mutationFn: () => post<RunSpec>('/containers/import-run', { command: cmd }),
    onSuccess: (spec) => {
      setForm(specToForm(spec))
      setImportError(null)
      setTab('form')
      toast.ok('Comando convertido. Revise os campos e crie o container.')
    },
    onError: (e) => setImportError(errorMessage(e)),
  })

  const run = useMutation({
    mutationFn: () => post<{ id: string }>('/containers/run', formToSpec(form)),
    onSuccess: () => {
      toast.ok(`Container ${form.name || form.image} criado.`)
      void qc.invalidateQueries({ queryKey: ['containers'] })
      void qc.invalidateQueries({ queryKey: ['images'] })
      onClose()
    },
  })

  return (
    <Modal
      open
      wide
      onClose={onClose}
      title="Novo container"
      footer={
        <>
          <Button onClick={onClose}>Cancelar</Button>
          <Button
            variant="primary"
            loading={run.isPending}
            onClick={() => {
              setTouched(true)
              if (!problem) run.mutate()
            }}
          >
            {run.isPending ? 'Baixando imagem e criando…' : 'Criar e iniciar'}
          </Button>
        </>
      }
    >
      <div className="mb-4 flex gap-2" role="tablist">
        {(['form', 'import'] as const).map((t) => (
          <button
            key={t}
            role="tab"
            aria-selected={tab === t}
            onClick={() => setTab(t)}
            className={cx('rounded-full px-3 py-1 text-sm', tab === t ? 'bg-accent text-accent-fg' : 'bg-surface-2 text-muted hover:text-fg')}
          >
            {t === 'form' ? 'Formulário' : 'Colar docker run'}
          </button>
        ))}
      </div>

      {tab === 'import' ? (
        <div className="space-y-3">
          <Field label="Comando docker run" hint="Suporta -d --name -p -v -e --restart --network -u -h --privileged.">
            <textarea className={cx(inputCls, 'h-32 font-mono text-xs')} value={cmd} onChange={(e) => setCmd(e.target.value)} placeholder="docker run -d --name web -p 8080:80 nginx:alpine" spellCheck={false} />
          </Field>
          {importError && (
            <p role="alert" className="rounded-xl border border-danger/40 bg-danger/10 p-2.5 text-sm text-danger">
              {importError}
            </p>
          )}
          <Button variant="primary" loading={importCmd.isPending} disabled={!cmd.trim()} onClick={() => importCmd.mutate()}>
            Converter
          </Button>
        </div>
      ) : (
        <div className="space-y-5">
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label="Imagem">
              <input className={inputCls} value={form.image} onChange={(e) => set('image', e.target.value)} placeholder="nginx:alpine" />
            </Field>
            <Field label="Nome (opcional)">
              <input className={inputCls} value={form.name} onChange={(e) => set('name', e.target.value)} placeholder="meu-container" />
            </Field>
            <Field label="Reiniciar automaticamente">
              <select className={inputCls} value={form.restart} onChange={(e) => set('restart', e.target.value)}>
                {RESTART.map((r) => (
                  <option key={r.v} value={r.v}>
                    {r.l}
                  </option>
                ))}
              </select>
            </Field>
          </div>

          <Rows
            title="Portas"
            addLabel="Adicionar porta"
            onAdd={() => set('ports', [...form.ports, { host: '', container: '', protocol: 'tcp' }])}
            rows={form.ports}
            render={(p, i) => (
              <>
                <input aria-label="Porta do servidor" className={inputCls} inputMode="numeric" placeholder="Servidor (ex. 8080)" value={p.host} onChange={(e) => set('ports', form.ports.map((x, j) => (j === i ? { ...x, host: e.target.value } : x)))} />
                <input aria-label="Porta do container" className={inputCls} inputMode="numeric" placeholder="Container (ex. 80)" value={p.container} onChange={(e) => set('ports', form.ports.map((x, j) => (j === i ? { ...x, container: e.target.value } : x)))} />
                <select aria-label="Protocolo" className={cx(inputCls, 'w-24')} value={p.protocol} onChange={(e) => set('ports', form.ports.map((x, j) => (j === i ? { ...x, protocol: e.target.value as 'tcp' | 'udp' } : x)))}>
                  <option>tcp</option>
                  <option>udp</option>
                </select>
              </>
            )}
            onRemove={(i) => set('ports', form.ports.filter((_, j) => j !== i))}
          />

          <Rows
            title="Volumes"
            addLabel="Adicionar volume"
            onAdd={() => set('volumes', [...form.volumes, { host: '', container: '', readOnly: false }])}
            rows={form.volumes}
            render={(v, i) => (
              <>
                <input aria-label="Pasta no servidor" className={inputCls} placeholder="/srv/dados" value={v.host} onChange={(e) => set('volumes', form.volumes.map((x, j) => (j === i ? { ...x, host: e.target.value } : x)))} />
                <input aria-label="Destino no container" className={inputCls} placeholder="/data" value={v.container} onChange={(e) => set('volumes', form.volumes.map((x, j) => (j === i ? { ...x, container: e.target.value } : x)))} />
                <label className="flex items-center gap-1 whitespace-nowrap text-xs text-muted">
                  <input type="checkbox" checked={v.readOnly} onChange={(e) => set('volumes', form.volumes.map((x, j) => (j === i ? { ...x, readOnly: e.target.checked } : x)))} />
                  somente leitura
                </label>
              </>
            )}
            onRemove={(i) => set('volumes', form.volumes.filter((_, j) => j !== i))}
          />

          <Rows
            title="Variáveis de ambiente"
            addLabel="Adicionar variável"
            onAdd={() => set('env', [...form.env, { key: '', value: '' }])}
            rows={form.env}
            render={(e, i) => (
              <>
                <input aria-label="Nome da variável" className={inputCls} placeholder="TZ" value={e.key} onChange={(ev) => set('env', form.env.map((x, j) => (j === i ? { ...x, key: ev.target.value } : x)))} />
                <input aria-label="Valor da variável" className={inputCls} placeholder="America/Sao_Paulo" value={e.value} onChange={(ev) => set('env', form.env.map((x, j) => (j === i ? { ...x, value: ev.target.value } : x)))} />
              </>
            )}
            onRemove={(i) => set('env', form.env.filter((_, j) => j !== i))}
          />

          {(form.privileged || form.network || form.user || form.hostname || form.command.length > 0) && (
            <p className="rounded-xl border border-line bg-surface-2 p-3 text-xs text-muted">
              Também será aplicado (do comando importado):{' '}
              {[form.privileged && 'privileged', form.network && `rede ${form.network}`, form.user && `usuário ${form.user}`, form.hostname && `hostname ${form.hostname}`, form.command.length > 0 && `comando “${form.command.join(' ')}”`]
                .filter(Boolean)
                .join(' · ')}
              .
            </p>
          )}

          {touched && problem && (
            <p role="alert" className="rounded-xl border border-danger/40 bg-danger/10 p-2.5 text-sm text-danger">
              {problem}
            </p>
          )}
          {run.isError && (
            <p role="alert" className="rounded-xl border border-danger/40 bg-danger/10 p-2.5 text-sm text-danger">
              {errorMessage(run.error)}
            </p>
          )}
        </div>
      )}
    </Modal>
  )
}

function Rows<T>({
  title, addLabel, rows, render, onAdd, onRemove,
}: {
  title: string
  addLabel: string
  rows: T[]
  render: (row: T, i: number) => React.ReactNode
  onAdd: () => void
  onRemove: (i: number) => void
}) {
  return (
    <fieldset className="space-y-2">
      <legend className="mb-1 text-sm font-medium">{title}</legend>
      {rows.map((r, i) => (
        <div key={i} className="flex items-center gap-2">
          {render(r, i)}
          <button type="button" aria-label={`Remover ${title.toLowerCase()} ${i + 1}`} onClick={() => onRemove(i)} className="rounded-lg p-2 text-muted hover:bg-surface-2 hover:text-danger">
            <Trash2 className="h-4 w-4" />
          </button>
        </div>
      ))}
      <Button variant="ghost" onClick={onAdd}>
        <Plus className="h-4 w-4" aria-hidden /> {addLabel}
      </Button>
    </fieldset>
  )
}
