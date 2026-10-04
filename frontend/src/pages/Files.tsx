import { FolderOpen } from 'lucide-react'
import { Card, EmptyState, PageHeader } from '../components/ui'

/** Placeholder: the file manager is specified in specs/07-file-manager.md and not built yet. */
export function Files() {
  return (
    <>
      <PageHeader title="Arquivos" subtitle="Navegue e organize os arquivos dos seus compartilhamentos." />
      <Card>
        <EmptyState icon={<FolderOpen className="mx-auto h-10 w-10" aria-hidden />} title="Em breve">
          O gerenciador de arquivos já está especificado e será implementado em uma próxima etapa. Enquanto isso, use os
          compartilhamentos de rede (SMB) ou a interface original do OMV.
        </EmptyState>
      </Card>
    </>
  )
}
