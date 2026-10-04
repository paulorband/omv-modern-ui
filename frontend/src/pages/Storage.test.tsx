import { afterEach, describe, expect, it, vi } from 'vitest'
import { screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Storage } from './Storage'
import { stepProblem } from '../components/ShareWizard'
import { mockApi, renderApp } from '../test-utils'
import type { Disk, Share } from '../lib/types'

const GIB = 1024 ** 3
const DISKS: Disk[] = [
  { uuid: 'd1f5c3a0', mntEntRef: '11111111-aaaa-4aaa-8aaa-111111111111', device: '/dev/sda1', label: 'data', type: 'ext4', mountPoint: '/srv/dev-disk-by-uuid-1111', total: 2000 * GIB, used: 760 * GIB, free: 1240 * GIB, percent: 38 },
  { uuid: 'd1f5c3b0', mntEntRef: '22222222-bbbb-4bbb-8bbb-222222222222', device: '/dev/sdb1', label: 'backup', type: 'ext4', mountPoint: '/srv/dev-disk-by-uuid-2222', total: 4000 * GIB, used: 3700 * GIB, free: 300 * GIB, percent: 92.5 },
]
const SHARES: Share[] = [
  { uuid: 'aaaaaaaa-0000-4000-8000-000000000001', name: 'media', path: '/srv/dev-disk-by-uuid-1111/media', mntEntRef: 'aaaa1111-0000-4000-8000-000000000001', comment: 'Filmes', smb: true, smbGuest: false, nfs: false },
]
const base = [
  { path: '/storage/filesystems', body: DISKS },
  { path: '/storage/shares', body: SHARES },
]

afterEach(() => vi.unstubAllGlobals())

describe('stepProblem (A06.5)', () => {
  const v = { fs: 'x', name: 'docs', path: '' }
  it('requires a disk on step 0', () => {
    expect(stepProblem(0, { ...v, fs: '' }, [])).toMatch(/disco|onde/i)
    expect(stepProblem(0, v, [])).toBeUndefined()
  })
  it('validates the name on step 1', () => {
    expect(stepProblem(1, { ...v, name: '' }, [])).toMatch(/Nome inválido/)
    expect(stepProblem(1, { ...v, name: 'has space' }, [])).toMatch(/Nome inválido/)
    expect(stepProblem(1, { ...v, name: 'Media' }, ['media'])).toMatch(/Já existe/)
    expect(stepProblem(1, v, [])).toBeUndefined()
  })
  it('rejects absolute and traversing folders', () => {
    expect(stepProblem(1, { ...v, path: '/etc' }, [])).toMatch(/relativa/)
    expect(stepProblem(1, { ...v, path: 'a/../b' }, [])).toMatch(/relativa/)
    expect(stepProblem(1, { ...v, path: 'sub/dir' }, [])).toBeUndefined()
  })
})

describe('Storage page', () => {
  it('shows disks with usage and warns when almost full (R06.10)', async () => {
    mockApi(base)
    renderApp(<Storage />)
    expect(await screen.findByText('backup')).toBeInTheDocument()
    expect(screen.getByRole('progressbar', { name: 'Uso de backup' })).toHaveAttribute('aria-valuenow', '93')
    expect(screen.getAllByText('quase cheio')).toHaveLength(1)
    expect(screen.getByRole('link', { name: /Configurações avançadas/ })).toHaveAttribute('target', '_blank')
  })

  it('lists shares and toggles SMB', async () => {
    const calls = mockApi([...base, { method: 'PATCH', path: '/storage/shares/aaaaaaaa-0000-4000-8000-000000000001', body: { ...SHARES[0], smb: false } }])
    renderApp(<Storage />)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('checkbox', { name: 'SMB de media' }))
    await waitFor(() => expect(calls.find((c) => c.method === 'PATCH')?.body).toEqual({ smb: false }))
  })

  it('removes a share and explains data is kept', async () => {
    const calls = mockApi([...base, { method: 'DELETE', path: '/storage/shares/aaaaaaaa-0000-4000-8000-000000000001', status: 204 }])
    renderApp(<Storage />)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Remover media' }))
    const dialog = screen.getByRole('dialog', { name: 'Remover media' })
    expect(within(dialog).getByText(/continuam no disco/)).toBeInTheDocument()
    await user.click(within(dialog).getByRole('button', { name: 'Remover compartilhamento' }))
    await waitFor(() => expect(calls.some((c) => c.method === 'DELETE')).toBe(true))
  })

  it('creates a share through the 3-step wizard', async () => {
    const calls = mockApi([...base, { method: 'POST', path: '/storage/shares', status: 201, body: { ...SHARES[0], name: 'docs' } }])
    renderApp(<Storage />)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: /Novo compartilhamento/ }))
    const dialog = await screen.findByRole('dialog', { name: 'Novo compartilhamento' })

    // step 1: cannot continue without a disk
    await user.click(within(dialog).getByRole('button', { name: 'Continuar' }))
    expect(within(dialog).getByRole('alert')).toBeInTheDocument()
    await user.click(within(dialog).getByRole('radio', { name: /data/ }))
    await user.click(within(dialog).getByRole('button', { name: 'Continuar' }))

    // step 2: invalid name blocks, existing name blocks, valid passes
    await user.type(within(dialog).getByLabelText('Nome do compartilhamento'), 'bad name')
    await user.click(within(dialog).getByRole('button', { name: 'Continuar' }))
    expect(within(dialog).getByRole('alert')).toHaveTextContent('Nome inválido')
    const name = within(dialog).getByLabelText('Nome do compartilhamento')
    await user.clear(name)
    await user.type(name, 'media')
    await user.click(within(dialog).getByRole('button', { name: 'Continuar' }))
    expect(within(dialog).getByRole('alert')).toHaveTextContent('Já existe')
    await user.clear(name)
    await user.type(name, 'docs')
    await user.click(within(dialog).getByRole('button', { name: 'Continuar' }))

    // step 3
    await user.click(within(dialog).getByRole('checkbox', { name: /sem senha/ }))
    await user.click(within(dialog).getByRole('button', { name: 'Criar compartilhamento' }))
    await waitFor(() => expect(calls.find((c) => c.method === 'POST')).toBeDefined())
    expect(calls.find((c) => c.method === 'POST')!.body).toEqual({
      name: 'docs', mntEntRef: DISKS[0].mntEntRef, relativePath: 'docs', comment: '', smb: true, smbGuest: true,
    })
  })

  it('shows the backend error inside the wizard', async () => {
    mockApi([...base, { method: 'POST', path: '/storage/shares', status: 502, body: { error: { code: 'upstream_error', message: 'falha na etapa "criar compartilhamento SMB" do OMV' } } }])
    renderApp(<Storage />)
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: /Novo compartilhamento/ }))
    const dialog = await screen.findByRole('dialog', { name: 'Novo compartilhamento' })
    await user.click(within(dialog).getByRole('radio', { name: /data/ }))
    await user.click(within(dialog).getByRole('button', { name: 'Continuar' }))
    await user.type(within(dialog).getByLabelText('Nome do compartilhamento'), 'docs')
    await user.click(within(dialog).getByRole('button', { name: 'Continuar' }))
    await user.click(within(dialog).getByRole('button', { name: 'Criar compartilhamento' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('criar compartilhamento SMB')
  })
})

describe('share location (real OMV behaviour)', () => {
  it('only offers disks that OMV can host shares on', async () => {
    const boot: Disk = { uuid: 'EACA-13DA', device: '/dev/mmcblk0p1', label: 'bootfs', type: 'vfat', mountPoint: '/boot/firmware', total: GIB, used: 1, free: GIB, percent: 15 } // no mntEntRef
    mockApi([{ path: '/storage/filesystems', body: [DISKS[0], boot] }, { path: '/storage/shares', body: [] }])
    renderApp(<Storage />)
    await userEvent.setup().click(await screen.findByRole('button', { name: /Novo compartilhamento/ }))
    const dialog = await screen.findByRole('dialog', { name: 'Novo compartilhamento' })
    expect(within(dialog).getAllByRole('radio')).toHaveLength(1)
    expect(within(dialog).queryByText('bootfs')).not.toBeInTheDocument()
  })

  it('disables share creation when no disk is eligible', async () => {
    mockApi([{ path: '/storage/filesystems', body: [{ ...DISKS[0], mntEntRef: undefined }] }, { path: '/storage/shares', body: [] }])
    renderApp(<Storage />)
    await screen.findByText('data')
    expect(screen.getByRole('button', { name: /Novo compartilhamento/ })).toBeDisabled()
  })
})
