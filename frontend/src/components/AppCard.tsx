import { useEffect, useRef, useState } from 'react'
import { FileText, MoreVertical, Play, RefreshCw, RotateCw, Square, Trash2 } from 'lucide-react'
import { appUrl } from '../lib/format'
import type { App } from '../lib/types'
import { AppIcon } from './AppIcon'
import { cx } from './ui'

export type AppAction = 'start' | 'stop' | 'restart' | 'logs' | 'update' | 'remove'

const STATE_LABEL: Record<App['state'], string> = { running: 'Em execução', stopped: 'Parado', partial: 'Parcial' }
const STATE_DOT: Record<App['state'], string> = { running: 'bg-ok', stopped: 'bg-muted', partial: 'bg-warn' }

export function AppCard({ app, busy, highlight, onAction }: { app: App; busy?: boolean; highlight?: boolean; onAction: (a: AppAction) => void }) {
  const [menu, setMenu] = useState(false)
  const root = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!menu) return
    const close = (e: MouseEvent | KeyboardEvent) => {
      if (e instanceof KeyboardEvent ? e.key === 'Escape' : !root.current?.contains(e.target as Node)) setMenu(false)
    }
    document.addEventListener('mousedown', close)
    document.addEventListener('keydown', close)
    return () => {
      document.removeEventListener('mousedown', close)
      document.removeEventListener('keydown', close)
    }
  }, [menu])

  const running = app.state !== 'stopped'
  const canOpen = app.port > 0
  const pick = (a: AppAction) => {
    setMenu(false)
    onAction(a)
  }

  const body = (
    <>
      <AppIcon icon={app.icon} name={app.name} className={cx('h-16 w-16', !running && 'opacity-50 grayscale')} />
      <div className="min-w-0 text-center">
        <p className={cx('truncate text-sm font-medium', !running && 'text-muted')}>{app.name}</p>
        <p className="flex items-center justify-center gap-1.5 text-xs text-muted">
          <span className={cx('inline-block h-2 w-2 rounded-full', STATE_DOT[app.state])} aria-hidden />
          {busy ? 'Aguarde…' : STATE_LABEL[app.state]}
        </p>
      </div>
    </>
  )
  const bodyCls = 'flex w-full flex-col items-center gap-2 rounded-2xl p-4 pt-5'

  return (
    <div
      ref={root}
      className={cx(
        'group relative rounded-2xl border border-line bg-surface shadow-card backdrop-blur-xl transition hover:-translate-y-0.5',
        highlight && 'ring-2 ring-accent',
      )}
    >
      {running && canOpen ? (
        <a href={appUrl(app)} target="_blank" rel="noreferrer" className={bodyCls} aria-label={`Abrir ${app.name}`}>
          {body}
        </a>
      ) : (
        <button
          type="button"
          className={bodyCls}
          onClick={() => onAction('start')}
          disabled={busy}
          aria-label={app.state === 'stopped' ? `Iniciar ${app.name}` : `Abrir ${app.name}`}
        >
          {body}
          {app.state === 'stopped' && <span className="text-xs font-medium text-accent">Iniciar</span>}
        </button>
      )}

      <button
        type="button"
        aria-label={`Ações de ${app.name}`}
        aria-haspopup="menu"
        aria-expanded={menu}
        onClick={() => setMenu((m) => !m)}
        className="absolute right-1.5 top-1.5 rounded-lg p-1.5 text-muted opacity-70 hover:bg-surface-2 hover:text-fg focus:opacity-100 group-hover:opacity-100"
      >
        <MoreVertical className="h-4 w-4" />
      </button>

      {menu && (
        <div role="menu" className="absolute right-1.5 top-9 z-20 w-44 overflow-hidden rounded-xl border border-line bg-surface-solid py-1 text-sm shadow-card">
          {app.state === 'stopped' ? (
            <Item icon={Play} label="Iniciar" onClick={() => pick('start')} />
          ) : (
            <Item icon={Square} label="Parar" onClick={() => pick('stop')} />
          )}
          <Item icon={RotateCw} label="Reiniciar" onClick={() => pick('restart')} />
          <Item icon={FileText} label="Ver logs" onClick={() => pick('logs')} />
          {app.managed && <Item icon={RefreshCw} label="Atualizar" onClick={() => pick('update')} />}
          <div className="my-1 border-t border-line" />
          <Item icon={Trash2} label="Remover" danger onClick={() => pick('remove')} />
        </div>
      )}
    </div>
  )
}

function Item({ icon: Icon, label, onClick, danger }: { icon: typeof Play; label: string; onClick: () => void; danger?: boolean }) {
  return (
    <button type="button" role="menuitem" onClick={onClick} className={cx('flex w-full items-center gap-2 px-3 py-2 text-left hover:bg-surface-2', danger && 'text-danger')}>
      <Icon className="h-4 w-4" aria-hidden />
      {label}
    </button>
  )
}
