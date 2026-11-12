// 时间统一按本地时区显示到分钟；后端的零值时间显示为“—”。
export function fmtTime(v?: string | null): string {
  if (!v || v.startsWith('0001-')) return '—'
  const d = new Date(v)
  if (Number.isNaN(d.getTime())) return '—'
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

// fmtAgo 给出“3 分钟前”这类相对时间，用在列表里。
export function fmtAgo(v?: string | null): string {
  if (!v || v.startsWith('0001-')) return '—'
  const sec = (Date.now() - new Date(v).getTime()) / 1000
  if (sec < 60) return '刚刚'
  if (sec < 3600) return `${Math.floor(sec / 60)} 分钟前`
  if (sec < 86400) return `${Math.floor(sec / 3600)} 小时前`
  if (sec < 86400 * 30) return `${Math.floor(sec / 86400)} 天前`
  return fmtTime(v)
}

// fmtUntil 给出“21 天后”这类到期时间；已经过了的写“已过期”。
export function fmtUntil(v?: string | null): string {
  if (!v || v.startsWith('0001-')) return '—'
  const sec = (new Date(v).getTime() - Date.now()) / 1000
  if (sec <= 0) return '已过期'
  if (sec < 3600) return `${Math.max(1, Math.floor(sec / 60))} 分钟后`
  if (sec < 86400) return `${Math.floor(sec / 3600)} 小时后`
  return `${Math.floor(sec / 86400)} 天后`
}

// copyText 复制到剪贴板。局域网 IP 走 http 时浏览器不提供 navigator.clipboard，退回到 execCommand。
export async function copyText(text: string) {
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(text)
    return
  }
  const ta = document.createElement('textarea')
  ta.value = text
  ta.setAttribute('readonly', '')
  ta.style.position = 'fixed'
  ta.style.opacity = '0'
  document.body.appendChild(ta)
  ta.select()
  const ok = document.execCommand('copy')
  ta.remove()
  if (!ok) throw new Error('浏览器禁止复制，需手动复制')
}

// 当前值在这个时长内到期算即将过期，和后端总览的口径一致。
export const expiringWithinMs = 3 * 86400_000

// expiryState 判断过期时间：已过期、即将过期，或都不是（null）。
export function expiryState(v?: string | null): 'expired' | 'expiring' | null {
  if (!v || v.startsWith('0001-')) return null
  const left = new Date(v).getTime() - Date.now()
  if (left <= 0) return 'expired'
  return left < expiringWithinMs ? 'expiring' : null
}

// 字段的名称和标识一起显示时，名称为空或和标识相同就只显示一次：返回还要另外显示的标识，不需要时返回空串。
export const secondaryKey = (name: string | undefined, key: string) =>
  name && name !== key ? key : ''
