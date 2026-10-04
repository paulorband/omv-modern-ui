import { useQuery } from '@tanstack/react-query'
import { get } from './api'

/** URL of the original OMV interface: same host the user is browsing, on OMV's configured port. */
export function omvUrlFor(port: number | undefined, loc: Pick<Location, 'protocol' | 'hostname'> = window.location): string {
  const standard = (loc.protocol === 'https:' && port === 443) || (loc.protocol === 'http:' && port === 80)
  return `${loc.protocol}//${loc.hostname}${port && !standard ? `:${port}` : ''}/`
}

export function useOmvAdminUrl(): string {
  const meta = useQuery({ queryKey: ['meta'], queryFn: ({ signal }) => get<{ omvPort: number }>('/meta', signal), staleTime: Infinity })
  return omvUrlFor(meta.data?.omvPort)
}
