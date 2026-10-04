import { useAuth } from '../lib/auth'
import { PageHeader } from '../components/ui'

export function Home() {
  const { user } = useAuth()
  return <PageHeader title={`Olá, ${user?.username}`} subtitle="Seu servidor, simples." />
}
