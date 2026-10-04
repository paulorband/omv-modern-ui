import type { EnvField } from './types'

/** Client-side mirror of the backend field validation (R05.6), for instant feedback. */
export function validateField(f: EnvField, raw: string): string | undefined {
  const v = raw.trim()
  if (!v) return f.required ? 'Campo obrigatório' : undefined
  if (/[\n\r\0]/.test(raw)) return 'Valor inválido'
  switch (f.type) {
    case 'path':
      if (!v.startsWith('/')) return 'Use um caminho absoluto (começando com /)'
      if (v.split('/').includes('..')) return 'O caminho não pode conter ..'
      return undefined
    case 'port': {
      const n = Number(v)
      return Number.isInteger(n) && n >= 1 && n <= 65535 ? undefined : 'Informe uma porta entre 1 e 65535'
    }
    case 'select':
      return f.options?.includes(v) ? undefined : 'Escolha uma das opções'
    case 'bool':
      return v === 'true' || v === 'false' ? undefined : 'Valor inválido'
    default:
      return v.length > 512 ? 'Valor longo demais' : undefined
  }
}

export function validatePort(raw: string): string | undefined {
  if (!raw.trim()) return undefined // empty = template default
  const n = Number(raw)
  return Number.isInteger(n) && n >= 1 && n <= 65535 ? undefined : 'Informe uma porta entre 1 e 65535'
}

/** Initial form values: template defaults. */
export function initialValues(fields: EnvField[]): Record<string, string> {
  return Object.fromEntries(fields.map((f) => [f.key, f.default ?? '']))
}
