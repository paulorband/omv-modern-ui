import { useAuth } from '../lib/auth'
import { SystemWidgets } from '../components/SystemWidgets'
import { PageHeader } from '../components/ui'

export function Home() {
  const { user } = useAuth()
  return (
    <>
      <PageHeader title={`Olá, ${user?.username}`} subtitle="Seu servidor, simples." />
      <SystemWidgets />
    </>
  )
}
