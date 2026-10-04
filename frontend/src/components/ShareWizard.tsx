import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { post } from '../lib/api'
import { formatBytes, SHARE_NAME_RE } from '../lib/format'
import type { Disk, Share } from '../lib/types'
import { useToast } from './Toast'
import { Button, Field, Modal, cx, errorMessage, inputCls } from './ui'

type Step = 0 | 1 | 2

/** Why the wizard cannot advance from `step`, or undefined (A06.5). */
export function stepProblem(step: Step, v: { fs: string; name: string; path: string }, existing: string[]): string | undefined {
  if (step === 0 && !v.fs) return 'Escolha onde guardar o compartilhamento.'
  if (step === 1) {
    if (!SHARE_NAME_RE.test(v.name)) return 'Nome inválido: use de 1 a 32 letras, números, _ ou -.'
    if (existing.some((n) => n.toLowerCase() === v.name.toLowerCase())) return 'Já existe um compartilhamento com esse nome.'
    const p = v.path.trim()
    if (p && (p.startsWith('/') || p.split('/').includes('..'))) return 'A pasta deve ser relativa ao disco, sem “..” e sem “/” no início.'
  }
  return undefined
}

const STEPS = ['Onde', 'Nome', 'Acesso']

export function ShareWizard({ disks, existing, onClose }: { disks: Disk[]; existing: string[]; onClose: () => void }) {
  const qc = useQueryClient()
  const toast = useToast()
  const [step, setStep] = useState<Step>(0)
  const [fs, setFs] = useState(disks.length === 1 ? disks[0].uuid : '')
  const [name, setName] = useState('')
  const [path, setPath] = useState('')
  const [comment, setComment] = useState('')
  const [smb, setSmb] = useState(true)
  const [guest, setGuest] = useState(false)
  const [shown, setShown] = useState(false) // show validation only after the first "Next"

  const problem = stepProblem(step, { fs, name, path }, existing)
  const disk = disks.find((d) => d.uuid === fs)

  const create = useMutation({
    mutationFn: () =>
      post<Share>('/storage/shares', { name, filesystemUuid: fs, relativePath: path.trim() || name, comment: comment.trim(), smb, smbGuest: smb && guest }),
    onSuccess: (s) => {
      toast.ok(`Compartilhamento ${s.name} criado.`)
      void qc.invalidateQueries({ queryKey: ['shares'] })
      onClose()
    },
  })

  const next = () => {
    setShown(true)
    if (problem) return
    setShown(false)
    setStep((s) => (s + 1) as Step)
  }

  return (
    <Modal
      open
      onClose={onClose}
      title="Novo compartilhamento"
      footer={
        <>
          {step > 0 && <Button onClick={() => setStep((s) => (s - 1) as Step)}>Voltar</Button>}
          {step < 2 ? (
            <Button variant="primary" onClick={next}>
              Continuar
            </Button>
          ) : (
            <Button variant="primary" loading={create.isPending} onClick={() => create.mutate()}>
              Criar compartilhamento
            </Button>
          )}
        </>
      }
    >
      <ol className="mb-5 flex items-center gap-2 text-xs" aria-label="Etapas">
        {STEPS.map((s, i) => (
          <li key={s} aria-current={i === step ? 'step' : undefined} className={cx('flex items-center gap-2', i === step ? 'font-semibold text-accent' : 'text-muted')}>
            <span className={cx('flex h-5 w-5 items-center justify-center rounded-full border text-[11px]', i <= step ? 'border-accent bg-accent/15' : 'border-line')}>{i + 1}</span>
            {s}
            {i < STEPS.length - 1 && <span aria-hidden>›</span>}
          </li>
        ))}
      </ol>

      {step === 0 && (
        <fieldset className="space-y-2">
          <legend className="mb-2 text-sm font-medium">Em qual disco?</legend>
          {disks.length === 0 && <p className="text-sm text-muted">Nenhum disco montado. Monte um filesystem na interface original do OMV.</p>}
          {disks.map((d) => (
            <label key={d.uuid} className={cx('flex cursor-pointer items-center gap-3 rounded-xl border p-3 text-sm', fs === d.uuid ? 'border-accent bg-accent/10' : 'border-line')}>
              <input type="radio" name="fs" checked={fs === d.uuid} onChange={() => setFs(d.uuid)} />
              <span className="flex-1">
                <span className="font-medium">{d.label || d.device}</span>
                <span className="block text-xs text-muted">
                  {formatBytes(d.free)} livres de {formatBytes(d.total)} · {d.mountPoint}
                </span>
              </span>
              {d.percent >= 90 && <span className="text-xs font-medium text-danger">quase cheio</span>}
            </label>
          ))}
        </fieldset>
      )}

      {step === 1 && (
        <div className="space-y-4">
          <Field label="Nome do compartilhamento" hint="É o nome que aparece na rede (ex.: \\servidor\filmes).">
            <input className={inputCls} value={name} onChange={(e) => setName(e.target.value)} autoFocus />
          </Field>
          <Field label="Pasta no disco (opcional)" hint={`Padrão: o próprio nome. Fica dentro de ${disk?.mountPoint ?? 'do disco'}.`}>
            <input className={inputCls} value={path} onChange={(e) => setPath(e.target.value)} placeholder={name || 'filmes'} />
          </Field>
          <Field label="Comentário (opcional)">
            <input className={inputCls} value={comment} onChange={(e) => setComment(e.target.value)} />
          </Field>
        </div>
      )}

      {step === 2 && (
        <div className="space-y-4 text-sm">
          <label className="flex items-start gap-3">
            <input type="checkbox" className="mt-1" checked={smb} onChange={(e) => setSmb(e.target.checked)} />
            <span>
              <span className="font-medium">Compartilhar na rede (SMB/Windows)</span>
              <span className="block text-xs text-muted">Acesso pelo Explorador de Arquivos, Finder e celulares.</span>
            </span>
          </label>
          <label className={cx('flex items-start gap-3', !smb && 'opacity-50')}>
            <input type="checkbox" className="mt-1" checked={smb && guest} disabled={!smb} onChange={(e) => setGuest(e.target.checked)} />
            <span>
              <span className="font-medium">Permitir acesso sem senha (convidado)</span>
              <span className="block text-xs text-muted">Qualquer pessoa na sua rede poderá ver e alterar os arquivos.</span>
            </span>
          </label>
          <dl className="rounded-xl border border-line bg-surface-2 p-3 text-xs">
            <div className="flex gap-2"><dt className="w-20 text-muted">Disco</dt><dd>{disk ? disk.label || disk.device : '—'}</dd></div>
            <div className="flex gap-2"><dt className="w-20 text-muted">Nome</dt><dd>{name}</dd></div>
            <div className="flex gap-2"><dt className="w-20 text-muted">Pasta</dt><dd>{path.trim() || name}</dd></div>
          </dl>
        </div>
      )}

      {shown && problem && (
        <p role="alert" className="mt-4 rounded-xl border border-danger/40 bg-danger/10 p-2.5 text-sm text-danger">
          {problem}
        </p>
      )}
      {create.isError && (
        <p role="alert" className="mt-4 rounded-xl border border-danger/40 bg-danger/10 p-2.5 text-sm text-danger">
          {errorMessage(create.error)}
        </p>
      )}
    </Modal>
  )
}
