import { useState } from 'react'
import { cx } from './ui'

const PALETTE = ['#3b6cf6', '#0ea5e9', '#14b8a6', '#22c55e', '#eab308', '#f97316', '#ec4899', '#8b5cf6']

function colorFor(name: string): string {
  let h = 0
  for (const ch of name) h = (h * 31 + ch.charCodeAt(0)) >>> 0
  return PALETTE[h % PALETTE.length]
}

/**
 * App icon: `emoji:<char>` renders the emoji, an http(s) URL renders an image, and anything
 * else (or an image that fails to load) falls back to a coloured avatar with the initial (R04.10).
 */
export function AppIcon({ icon, name, className }: { icon?: string; name: string; className?: string }) {
  const [failed, setFailed] = useState(false)
  const base = cx('flex shrink-0 items-center justify-center overflow-hidden rounded-2xl', className ?? 'h-14 w-14')

  if (icon?.startsWith('emoji:')) {
    return (
      <div className={cx(base, 'bg-surface-2 text-3xl')} aria-hidden>
        {icon.slice('emoji:'.length)}
      </div>
    )
  }
  if (icon && /^https?:\/\//.test(icon) && !failed) {
    return (
      <div className={cx(base, 'bg-surface-2')}>
        <img src={icon} alt="" className="h-full w-full object-cover" onError={() => setFailed(true)} loading="lazy" />
      </div>
    )
  }
  const initial = (name.trim()[0] ?? '?').toUpperCase()
  return (
    <div className={cx(base, 'text-xl font-semibold text-white')} style={{ background: colorFor(name) }} aria-hidden data-testid="icon-fallback">
      {initial}
    </div>
  )
}
