import type { RunSpec } from './types'

export type PortRow = { host: string; container: string; protocol: 'tcp' | 'udp' }
export type VolumeRow = { host: string; container: string; readOnly: boolean }
export type EnvRow = { key: string; value: string }

export type RunForm = {
  image: string
  name: string
  restart: string
  ports: PortRow[]
  volumes: VolumeRow[]
  env: EnvRow[]
  // Only populated by "import docker run"; kept so nothing the user pasted is silently lost.
  network: string
  user: string
  hostname: string
  privileged: boolean
  command: string[]
}

export const emptyForm = (): RunForm => ({
  image: '', name: '', restart: 'unless-stopped', ports: [], volumes: [], env: [],
  network: '', user: '', hostname: '', privileged: false, command: [],
})

/** Converts the API spec (e.g. from import-run) into editable form state. */
export function specToForm(s: RunSpec): RunForm {
  return {
    image: s.image,
    name: s.name ?? '',
    restart: s.restart ?? '',
    ports: (s.ports ?? []).map((p) => ({ host: p.host ? String(p.host) : '', container: String(p.container), protocol: p.protocol === 'udp' ? 'udp' : 'tcp' })),
    volumes: (s.volumes ?? []).map((v) => ({ host: v.host, container: v.container, readOnly: v.readOnly })),
    env: Object.entries(s.env ?? {}).map(([key, value]) => ({ key, value })),
    network: s.network ?? '',
    user: s.user ?? '',
    hostname: s.hostname ?? '',
    privileged: !!s.privileged,
    command: s.command ?? [],
  }
}

/** Builds the request body, dropping blank rows. */
export function formToSpec(f: RunForm): RunSpec {
  const spec: RunSpec = { image: f.image.trim() }
  if (f.name.trim()) spec.name = f.name.trim()
  if (f.restart) spec.restart = f.restart
  const ports = f.ports.filter((p) => p.container.trim())
  if (ports.length) spec.ports = ports.map((p) => ({ host: Number(p.host) || 0, container: Number(p.container), protocol: p.protocol }))
  const volumes = f.volumes.filter((v) => v.host.trim() && v.container.trim())
  if (volumes.length) spec.volumes = volumes.map((v) => ({ host: v.host.trim(), container: v.container.trim(), readOnly: v.readOnly }))
  const env = f.env.filter((e) => e.key.trim())
  if (env.length) spec.env = Object.fromEntries(env.map((e) => [e.key.trim(), e.value]))
  if (f.network) spec.network = f.network
  if (f.user) spec.user = f.user
  if (f.hostname) spec.hostname = f.hostname
  if (f.privileged) spec.privileged = true
  if (f.command.length) spec.command = f.command
  return spec
}

/** First problem with the form, or undefined (mirrors backend validation). */
export function validateForm(f: RunForm): string | undefined {
  if (!f.image.trim() || /\s/.test(f.image.trim())) return 'Informe a imagem (ex.: nginx:alpine)'
  if (f.name.trim() && !/^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/.test(f.name.trim())) return 'Nome inválido: use letras, números, _ . -'
  for (const p of f.ports) {
    if (!p.container.trim() && !p.host.trim()) continue
    const c = Number(p.container)
    const h = p.host.trim() ? Number(p.host) : 0
    if (!Number.isInteger(c) || c < 1 || c > 65535 || !Number.isInteger(h) || h < 0 || h > 65535) return 'Portas devem estar entre 1 e 65535'
  }
  for (const v of f.volumes) {
    if (!v.host.trim() && !v.container.trim()) continue
    if (!v.host.trim() || !v.container.trim().startsWith('/')) return 'Volumes precisam de origem e de um destino absoluto (/...)'
  }
  for (const e of f.env) {
    if (e.key.trim() && !/^[a-zA-Z_][a-zA-Z0-9_]*$/.test(e.key.trim())) return `Variável inválida: ${e.key}`
  }
  return undefined
}
