import { useEffect, useRef, useState } from 'react'
import { subscribe } from './sse'
import type { NetIface, Stats } from './types'

export type NetRate = { rx: number; tx: number }

/** Bytes per second across all interfaces between two samples. */
export function networkRate(prev: Stats | null, cur: Stats | null): NetRate | null {
  if (!prev || !cur) return null
  const dt = (new Date(cur.time).getTime() - new Date(prev.time).getTime()) / 1000
  if (!(dt > 0)) return null
  const sum = (n: NetIface[] | null, k: 'rxBytes' | 'txBytes') => (n ?? []).reduce((a, i) => a + i[k], 0)
  const drx = sum(cur.network, 'rxBytes') - sum(prev.network, 'rxBytes')
  const dtx = sum(cur.network, 'txBytes') - sum(prev.network, 'txBytes')
  if (drx < 0 || dtx < 0) return null // counters reset (interface restarted)
  return { rx: drx / dt, tx: dtx / dt }
}

/** Live host metrics from the SSE stream (R02.3). */
export function useStats() {
  const [stats, setStats] = useState<Stats | null>(null)
  const [rate, setRate] = useState<NetRate | null>(null)
  const [connected, setConnected] = useState(false)
  const prev = useRef<Stats | null>(null)

  useEffect(() => {
    return subscribe<Stats>(
      '/system/stats/stream',
      {
        stats: (s) => {
          setConnected(true)
          setRate(networkRate(prev.current, s))
          prev.current = s
          setStats(s)
        },
      },
      () => setConnected(false),
    )
  }, [])

  return { stats, rate, connected }
}
