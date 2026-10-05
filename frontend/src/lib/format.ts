const UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']

export function formatBytes(n: number, digits = 1): string {
  if (!Number.isFinite(n) || n <= 0) return '0 B'
  const i = Math.max(0, Math.min(Math.floor(Math.log(n) / Math.log(1024)), UNITS.length - 1))
  const v = n / 1024 ** i
  return `${i === 0 ? Math.round(v) : v.toFixed(digits)} ${UNITS[i]}`
}

export function formatUptime(seconds: number): string {
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}min`
  return `${m}min`
}

export function formatRate(bytesPerSecond: number): string {
  return `${formatBytes(bytesPerSecond)}/s`
}

export function percent(part: number, total: number): number {
  return total > 0 ? Math.min(100, Math.max(0, (part / total) * 100)) : 0
}

/** Share names: 1-32 letters, digits, "_" or "-" (matches the backend). */
export const SHARE_NAME_RE = /^[a-zA-Z0-9_-]{1,32}$/

/** Builds the URL that opens an app, using the host the user is browsing with (R04.2). */
export function appUrl(app: { port: number; scheme: string; path: string }, hostname = window.location.hostname): string {
  const path = app.path ? (app.path.startsWith('/') ? app.path : '/' + app.path) : ''
  const host = hostname.includes(':') && !hostname.startsWith('[') ? `[${hostname}]` : hostname // IPv6
  return `${app.scheme || 'http'}://${host}:${app.port}${path}`
}
