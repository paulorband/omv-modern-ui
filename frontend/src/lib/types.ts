// Mirrors the JSON returned by the Go API (see specs/).

export type User = { username: string; role: string }

export type SystemInfo = {
  hostname: string
  omvVersion: string
  kernel: string
  uptimeSeconds: number
  cores: number
}

export type NetIface = { name: string; rxBytes: number; txBytes: number }

export type Stats = {
  time: string
  cpu: { percent: number; cores: number; load: [number, number, number] }
  memory: { total: number; used: number; available: number }
  swap: { total: number; used: number }
  temperature: number | null
  network: NetIface[] | null
}

export type Disk = {
  uuid: string
  device: string
  label: string
  type: string
  mountPoint: string
  total: number
  used: number
  free: number
  percent: number
  /** Mount-entry id needed to create shares there; absent when OMV does not offer the disk. */
  mntEntRef?: string
}

export type Port = { private: number; public: number; protocol: string }

export type Container = {
  id: string
  name: string
  image: string
  state: string
  status: string
  ports: Port[]
  createdAt: string
  appId?: string
}

export type EnvVar = { key: string; value: string; masked: boolean }

export type ContainerDetail = Container & {
  env: EnvVar[]
  mounts: { type: string; source: string; destination: string; readOnly: boolean }[]
  networks: string[]
  restartPolicy: string
  tty: boolean
  command: string[] | null
}

export type RunSpec = {
  image: string
  name?: string
  ports?: { hostIp?: string; host: number; container: number; protocol?: string }[]
  volumes?: { host: string; container: string; readOnly: boolean }[]
  env?: Record<string, string>
  restart?: string
  network?: string
  user?: string
  hostname?: string
  privileged?: boolean
  command?: string[]
}

export type DockerImage = { id: string; tags: string[]; size: number; createdAt: string; inUse: boolean }

export type DockerStatus = {
  available: boolean
  composeAvailable: boolean
  version?: string
  apiVersion?: string
  error?: string
}

export type AppState = 'running' | 'stopped' | 'partial'

export type App = {
  id: string
  name: string
  icon: string
  category: string
  state: AppState
  port: number
  scheme: string
  path: string
  managed: boolean
  containers: string[]
}

export type StoreCard = {
  id: string
  title: string
  tagline: string
  icon: string
  category: string
  installed: boolean
}

export type EnvField = {
  key: string
  label: string
  type: 'string' | 'path' | 'port' | 'password' | 'bool' | 'select'
  default: string
  required: boolean
  options?: string[]
}

export type StoreDetail = {
  meta: {
    id: string
    title: string
    tagline: string
    description: string
    icon: string
    category: string
    port: number
    main: string
    env: EnvField[] | null
  }
  compose: string
  installed: boolean
}

export type InstallEvent = { type: 'starting' | 'pull' | 'log' | 'done' | 'error'; message?: string }

export type Share = {
  uuid: string
  name: string
  path: string
  mntEntRef: string
  comment: string
  smb: boolean
  smbGuest: boolean
  nfs: boolean
}

export type OmvUser = { name: string; uid: number; comment: string }
