import { useAuth } from '../lib/auth'
import { AppsGrid } from '../components/AppsGrid'
import { SystemWidgets } from '../components/SystemWidgets'
import { PageHeader } from '../components/ui'

export function Home() {
  const { user } = useAuth()
  return (
    <div className="space-y-8">
      <PageHeader title={`Olá, ${user?.username}`} subtitle="Seu servidor, simples." />
      <SystemWidgets />
      <AppsGrid />
    </div>
  )
}
