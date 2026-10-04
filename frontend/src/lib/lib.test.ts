import { describe, expect, it } from 'vitest'
import { appUrl, formatBytes, formatUptime, percent, SHARE_NAME_RE } from './format'
import { parseSSE } from './sse'
import { omvUrlFor } from './useOmvUrl'

describe('format', () => {
  it('formats bytes', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(1536)).toBe('1.5 KB')
    expect(formatBytes(5 * 1024 ** 3)).toBe('5.0 GB')
    expect(formatBytes(-1)).toBe('0 B')
  })

  it('formats uptime', () => {
    expect(formatUptime(59)).toBe('0min')
    expect(formatUptime(3700)).toBe('1h 1min')
    expect(formatUptime(90000)).toBe('1d 1h')
  })

  it('clamps percentages', () => {
    expect(percent(50, 200)).toBe(25)
    expect(percent(5, 0)).toBe(0)
    expect(percent(300, 100)).toBe(100)
  })

  it('validates share names like the backend', () => {
    expect(SHARE_NAME_RE.test('media_01-x')).toBe(true)
    expect(SHARE_NAME_RE.test('has space')).toBe(false)
    expect(SHARE_NAME_RE.test('')).toBe(false)
    expect(SHARE_NAME_RE.test('a'.repeat(33))).toBe(false)
  })

  it('builds app URLs from the browsing host (R04.2)', () => {
    expect(appUrl({ port: 8096, scheme: 'http', path: '' }, '192.168.1.10')).toBe('http://192.168.1.10:8096')
    expect(appUrl({ port: 8443, scheme: 'https', path: 'web' }, 'omv.local')).toBe('https://omv.local:8443/web')
    expect(appUrl({ port: 80, scheme: '', path: '/x' }, 'fe80::1')).toBe('http://[fe80::1]:80/x')
  })
})

describe('parseSSE', () => {
  it('splits events and keeps the incomplete tail', () => {
    const { events, rest } = parseSSE('event: log\ndata: {"a":1}\n\n: keep-alive\n\nevent: done\ndata: {"b":2}\n\nevent: part')
    expect(events).toEqual([
      { event: 'log', data: '{"a":1}' },
      { event: 'done', data: '{"b":2}' },
    ])
    expect(rest).toBe('event: part')
  })

  it('defaults the event name and joins multi-line data', () => {
    const { events } = parseSSE('data: x\ndata: y\n\n')
    expect(events).toEqual([{ event: 'message', data: 'x\ny' }])
  })
})

describe('omvUrlFor', () => {
  const http = { protocol: 'http:', hostname: '192.168.1.5' }
  it('uses the configured OMV port', () => {
    expect(omvUrlFor(81, http)).toBe('http://192.168.1.5:81/')
    expect(omvUrlFor(80, http)).toBe('http://192.168.1.5/')
    expect(omvUrlFor(443, { protocol: 'https:', hostname: 'omv.local' })).toBe('https://omv.local/')
    expect(omvUrlFor(8443, { protocol: 'https:', hostname: 'omv.local' })).toBe('https://omv.local:8443/')
  })
  it('falls back to the bare host while the port is unknown', () => {
    expect(omvUrlFor(undefined, http)).toBe('http://192.168.1.5/')
  })
})
