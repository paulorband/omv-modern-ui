import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { get, put } from '../lib/api'
import type { App } from '../lib/types'
import { useToast } from './Toast'
import { Button, ErrorBox, Field, Modal, Spinner, errorMessage, inputCls } from './ui'

type Files = { compose: string; env: string }

/** Edits the compose and .env of a managed app and redeploys it. */
export function EditAppDialog({ app, onClose }: { app: App; onClose: () => void }) {
  const files = useQuery({ queryKey: ['app-compose', app.id], queryFn: ({ signal }) => get<Files>(`/apps/${app.id}/compose`, signal), gcTime: 0 })
  return (
    <Modal open onClose={onClose} title={`Editar ${app.name}`} wide>
      {files.isPending ? <Spinner /> : files.isError ? <ErrorBox error={files.error} onRetry={() => void files.refetch()} /> : <Form app={app} initial={files.data} onClose={onClose} />}
    </Modal>
  )
}

function Form({ app, initial, onClose }: { app: App; initial: Files; onClose: () => void }) {
  const qc = useQueryClient()
  const toast = useToast()
  const [compose, setCompose] = useState(initial.compose)
  const [env, setEnv] = useState(initial.env)
  const [allowUnsafe, setAllowUnsafe] = useState(false)
  const [error, setError] = useState('')
  const save = useMutation({
    mutationFn: () => put(`/apps/${app.id}/compose`, { compose, env, allowUnsafe }),
    onSuccess: () => {
      toast.ok(`${app.name} atualizado e reimplantado.`)
      onClose()
    },
    onError: (e) => setError(errorMessage(e)),
    onSettled: () => void qc.invalidateQueries({ queryKey: ['apps'] }),
  })
  const unsafe = error.includes('inseguras')

  return (
    <form
      className="space-y-4 text-sm"
      onSubmit={(e) => {
        e.preventDefault()
        setError('')
        save.mutate()
      }}
    >
      <Field label="docker-compose.yml" hint="Mantenha o label omvhome.app, senão o app some da lista.">
        <textarea className={`${inputCls} h-72 font-mono text-xs`} spellCheck={false} value={compose} onChange={(e) => setCompose(e.target.value)} />
      </Field>
      <Field label=".env" hint="Variáveis usadas pelo compose (podem conter senhas).">
        <textarea className={`${inputCls} h-28 font-mono text-xs`} spellCheck={false} value={env} onChange={(e) => setEnv(e.target.value)} />
      </Field>
      {unsafe && (
        <label className="flex items-start gap-2">
          <input type="checkbox" className="mt-1" checked={allowUnsafe} onChange={(e) => setAllowUnsafe(e.target.checked)} />
          <span>Entendo os riscos e quero permitir essas configurações.</span>
        </label>
      )}
      {error && (
        <p role="alert" className="text-danger">
          {error}
        </p>
      )}
      <div className="flex justify-end gap-2">
        <Button onClick={onClose}>Cancelar</Button>
        <Button type="submit" variant="primary" loading={save.isPending}>
          Salvar e reimplantar
        </Button>
      </div>
    </form>
  )
}
