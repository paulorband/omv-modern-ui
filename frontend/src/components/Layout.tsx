import { NavLink, Outlet } from 'react-router-dom'
import { ExternalLink, FolderOpen, HardDrive, Home, LogOut, Package, Store } from 'lucide-react'
import { useAuth } from '../lib/auth'
import { useOmvAdminUrl } from '../lib/useOmvUrl'
import { cx } from './ui'

const NAV = [
  { to: '/', label: 'Início', icon: Home, end: true },
  { to: '/store', label: 'Loja', icon: Store },
  { to: '/containers', label: 'Containers', icon: Package },
  { to: '/storage', label: 'Armazenamento', icon: HardDrive },
  { to: '/files', label: 'Arquivos', icon: FolderOpen },
]

export function Layout() {
  const { user, logout } = useAuth()
  const omvUrl = useOmvAdminUrl()
  const item = ({ isActive }: { isActive: boolean }) =>
    cx(
      'flex items-center gap-2 rounded-xl px-3 py-2 text-sm font-medium transition',
      isActive ? 'bg-accent/15 text-accent' : 'text-muted hover:bg-surface-2 hover:text-fg',
    )

  return (
    <div className="mx-auto flex min-h-screen max-w-6xl flex-col px-4 pb-24 sm:pb-8">
      <header className="flex items-center justify-between gap-4 py-4">
        <div className="flex items-center gap-2 text-lg font-semibold">
          <span aria-hidden className="text-2xl">
            🏠
          </span>
          OMV Home
        </div>

        <nav aria-label="Principal" className="hidden gap-1 sm:flex">
          {NAV.map(({ to, label, icon: Icon, end }) => (
            <NavLink key={to} to={to} end={end} className={item}>
              <Icon className="h-4 w-4" aria-hidden />
              {label}
            </NavLink>
          ))}
        </nav>

        <div className="flex items-center gap-1">
          <a
            href={omvUrl}
            target="_blank"
            rel="noreferrer"
            className="hidden items-center gap-1.5 rounded-xl px-3 py-2 text-sm text-muted hover:bg-surface-2 hover:text-fg md:flex"
          >
            <ExternalLink className="h-4 w-4" aria-hidden />
            Configurações avançadas
          </a>
          <button
            type="button"
            onClick={() => void logout()}
            className="flex items-center gap-1.5 rounded-xl px-3 py-2 text-sm text-muted hover:bg-surface-2 hover:text-fg"
            title={`Sair (${user?.username})`}
          >
            <LogOut className="h-4 w-4" aria-hidden />
            <span className="hidden sm:inline">Sair</span>
          </button>
        </div>
      </header>

      <main className="flex-1">
        <Outlet />
      </main>

      <nav aria-label="Principal (celular)" className="fixed inset-x-0 bottom-0 z-40 flex justify-around border-t border-line bg-surface-solid px-2 py-1.5 sm:hidden">
        {NAV.map(({ to, label, icon: Icon, end }) => (
          <NavLink
            key={to}
            to={to}
            end={end}
            className={({ isActive }) =>
              cx('flex flex-col items-center gap-0.5 rounded-lg px-2 py-1 text-[11px]', isActive ? 'text-accent' : 'text-muted')
            }
          >
            <Icon className="h-5 w-5" aria-hidden />
            {label}
          </NavLink>
        ))}
      </nav>
    </div>
  )
}
