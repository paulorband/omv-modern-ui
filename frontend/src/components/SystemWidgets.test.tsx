import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, screen, within } from '@testing-library/react'
import { SystemWidgets } from './SystemWidgets'
import { networkRate } from '../lib/useStats'
import { FakeEventSource, mockApi, renderApp } from '../test-utils'
import type { Stats } from '../lib/types'

const stat = (time: string, rx: number, tx: number, cpu = 12): Stats => ({
  time,
  cpu: { percent: cpu, cores: 4, load: [0.5, 0.4, 0.3] },
  memory: { total: 8 * 1024 ** 3, used: 2 * 1024 ** 3, available: 6 * 1024 ** 3 },
  swap: { total: 0, used: 0 },
  temperature: 41,
  network: [{ name: 'eth0', rxBytes: rx, txBytes: tx }],
})

describe('networkRate', () => {
  it('computes bytes per second between samples', () => {
    const r = networkRate(stat('2026-01-01T00:00:00Z', 1000, 500), stat('2026-01-01T00:00:02Z', 5000, 1500))
    expect(r).toEqual({ rx: 2000, tx: 500 })
  })
  it('ignores counter resets and missing samples', () => {
    expect(networkRate(null, stat('2026-01-01T00:00:00Z', 1, 1))).toBeNull()
    expect(networkRate(stat('2026-01-01T00:00:00Z', 9000, 9000), stat('2026-01-01T00:00:02Z', 10, 10))).toBeNull()
    expect(networkRate(stat('2026-01-01T00:00:00Z', 1, 1), stat('2026-01-01T00:00:00Z', 2, 2))).toBeNull()
  })
})

describe('SystemWidgets', () => {
  beforeEach(() => {
    FakeEventSource.reset()
    vi.stubGlobal('EventSource', FakeEventSource)
  })
  afterEach(() => vi.unstubAllGlobals())

  it('shows the four widgets with live data (A02.5)', async () => {
    mockApi([
      { path: '/auth/me', body: { user: { username: 'admin', role: 'admin' } } },
      { path: '/system', body: { hostname: 'omv-demo', omvVersion: '7.4', kernel: 'x', uptimeSeconds: 7200, cores: 4 } },
      {
        path: '/system/disks',
        body: [
          { uuid: 'a', device: '/dev/sda1', label: 'data', type: 'ext4', mountPoint: '/srv/a', total: 2000 * 1024 ** 3, used: 760 * 1024 ** 3, free: 1, percent: 38 },
          { uuid: 'b', device: '/dev/sdb1', label: 'backup', type: 'ext4', mountPoint: '/srv/b', total: 4000 * 1024 ** 3, used: 3700 * 1024 ** 3, free: 1, percent: 92.5 },
        ],
      },
    ])
    renderApp(<SystemWidgets />)

    expect(await screen.findByText('omv-demo')).toBeInTheDocument()
    expect(screen.getByText('Processador')).toBeInTheDocument()
    expect(screen.getByText('Memória')).toBeInTheDocument()
    expect(screen.getByText('Rede')).toBeInTheDocument()
    expect(screen.getByText('Armazenamento')).toBeInTheDocument()

    const es = FakeEventSource.last('/system/stats/stream')!
    act(() => es.emit('stats', stat('2026-01-01T00:00:00Z', 0, 0, 37)))
    act(() => es.emit('stats', stat('2026-01-01T00:00:02Z', 2_000_000, 400_000, 37)))

    expect(await screen.findByText('37%')).toBeInTheDocument()
    expect(screen.getByText('25%')).toBeInTheDocument() // memory
    expect(screen.getByText(/976\.6 KB\/s/)).toBeInTheDocument() // rx rate
    expect(screen.getByText(/41 °C/)).toBeInTheDocument()

    const cpuBar = screen.getByRole('progressbar', { name: 'Uso de CPU' })
    expect(cpuBar).toHaveAttribute('aria-valuenow', '37')
    expect(within(screen.getByText('backup').closest('li')!).getByText(/quase cheio/)).toBeInTheDocument()
    expect(screen.getByRole('progressbar', { name: 'Uso de data' })).toHaveAttribute('aria-valuenow', '38')
  })

  it('shows an error with retry when disks fail to load', async () => {
    mockApi([
      { path: '/system', body: { hostname: 'h', omvVersion: '', kernel: '', uptimeSeconds: 0, cores: 1 } },
      { path: '/system/disks', status: 502, body: { error: { code: 'upstream_error', message: 'falha ao consultar o OMV' } } },
    ])
    renderApp(<SystemWidgets />)
    expect(await screen.findByRole('alert')).toHaveTextContent('falha ao consultar o OMV')
  })

  it('closes the stream on unmount', () => {
    mockApi([])
    const { unmount } = renderApp(<SystemWidgets />)
    const es = FakeEventSource.last('/system/stats/stream')!
    unmount()
    expect(es.closed).toBe(true)
  })
})
