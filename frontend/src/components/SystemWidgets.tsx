import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Activity, ArrowDown, ArrowUp, Cpu, HardDrive, MemoryStick, Thermometer } from 'lucide-react'
import { get } from '../lib/api'
import { formatBytes, formatRate, formatUptime, percent } from '../lib/format'
import { useStats } from '../lib/useStats'
import type { Disk, SystemInfo } from '../lib/types'
import { Badge, Card, ErrorBox, ProgressBar } from './ui'

function Widget({ title, icon, children }: { title: string; icon: ReactNode; children: ReactNode }) {
  return (
    <Card className="p-4">
      <div className="mb-3 flex items-center gap-2 text-sm font-medium text-muted">
        {icon}
        {title}
      </div>
      {children}
    </Card>
  )
}

export function SystemWidgets() {
  const { stats, rate, connected } = useStats()
  const info = useQuery({ queryKey: ['system'], queryFn: ({ signal }) => get<SystemInfo>('/system', signal), refetchInterval: 60_000 })
  const disks = useQuery({ queryKey: ['disks'], queryFn: ({ signal }) => get<Disk[]>('/system/disks', signal), refetchInterval: 30_000 })

  const memPct = stats ? percent(stats.memory.used, stats.memory.total) : 0

  return (
    <section aria-label="Sistema" className="space-y-3">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted">
        {info.data && (
          <>
            <span className="font-medium text-fg">{info.data.hostname}</span>
            {info.data.omvVersion && <span>OMV {info.data.omvVersion}</span>}
            <span>ligado há {formatUptime(info.data.uptimeSeconds)}</span>
          </>
        )}
        {!connected && stats && <Badge tone="warn">reconectando…</Badge>}
      </div>

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <Widget title="Processador" icon={<Cpu className="h-4 w-4" aria-hidden />}>
          <div className="mb-2 flex items-baseline justify-between">
            <span className="text-2xl font-semibold tabular-nums">{stats ? `${stats.cpu.percent.toFixed(0)}%` : '—'}</span>
            {stats?.temperature != null && (
              <span className="flex items-center gap-1 text-xs text-muted">
                <Thermometer className="h-3.5 w-3.5" aria-hidden />
                {stats.temperature.toFixed(0)} °C
              </span>
            )}
          </div>
          <ProgressBar value={stats?.cpu.percent ?? 0} label="Uso de CPU" />
          <p className="mt-2 text-xs text-muted">
            {stats ? `Carga ${stats.cpu.load.map((l) => l.toFixed(2)).join(' · ')} · ${stats.cpu.cores} núcleos` : 'Aguardando dados…'}
          </p>
        </Widget>

        <Widget title="Memória" icon={<MemoryStick className="h-4 w-4" aria-hidden />}>
          <div className="mb-2 text-2xl font-semibold tabular-nums">{stats ? `${memPct.toFixed(0)}%` : '—'}</div>
          <ProgressBar value={memPct} label="Uso de memória" />
          <p className="mt-2 text-xs text-muted">
            {stats ? `${formatBytes(stats.memory.used)} de ${formatBytes(stats.memory.total)}` : 'Aguardando dados…'}
            {stats && stats.swap.total > 0 && ` · swap ${formatBytes(stats.swap.used)}`}
          </p>
        </Widget>

        <Widget title="Rede" icon={<Activity className="h-4 w-4" aria-hidden />}>
          <div className="space-y-1 tabular-nums">
            <div className="flex items-center gap-2 text-lg font-semibold">
              <ArrowDown className="h-4 w-4 text-ok" aria-label="Recebido" />
              {rate ? formatRate(rate.rx) : '—'}
            </div>
            <div className="flex items-center gap-2 text-lg font-semibold">
              <ArrowUp className="h-4 w-4 text-accent" aria-label="Enviado" />
              {rate ? formatRate(rate.tx) : '—'}
            </div>
          </div>
          <p className="mt-2 text-xs text-muted">
            {stats?.network?.length ? `${stats.network.map((n) => n.name).join(', ')}` : 'Sem interfaces'}
          </p>
        </Widget>

        <Widget title="Armazenamento" icon={<HardDrive className="h-4 w-4" aria-hidden />}>
          {disks.isError ? (
            <ErrorBox error={disks.error} onRetry={() => void disks.refetch()} />
          ) : !disks.data ? (
            <p className="text-sm text-muted">Carregando…</p>
          ) : disks.data.length === 0 ? (
            <p className="text-sm text-muted">Nenhum disco montado.</p>
          ) : (
            <ul className="space-y-3">
              {disks.data.map((d) => (
                <li key={d.uuid}>
                  <div className="mb-1 flex items-center justify-between text-sm">
                    <span className="truncate font-medium">{d.label || d.device}</span>
                    <span className="tabular-nums text-muted">{d.percent.toFixed(0)}%</span>
                  </div>
                  <ProgressBar value={d.percent} label={`Uso de ${d.label || d.device}`} />
                  <p className="mt-1 text-xs text-muted">
                    {formatBytes(d.used)} de {formatBytes(d.total)}
                    {d.percent >= 90 && <span className="ml-1 font-medium text-danger">· quase cheio</span>}
                  </p>
                </li>
              ))}
            </ul>
          )}
        </Widget>
      </div>
    </section>
  )
}
