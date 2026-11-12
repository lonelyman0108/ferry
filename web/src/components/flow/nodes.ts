import { fmtAgo, fmtUntil } from '@/format'
import type { Tone } from '@/lib/tone'
import type { Flow, FlowBinding, FlowDestination, FlowDevice, OverviewSource } from '@/types'

// 流向图节点卡和连线的口径，全图和聚焦窄栏共用。

// 连线状态：正常、失败、等待重试、没连通或没推过。
export type EdgeState = 'ok' | 'fail' | 'warn' | 'idle'

export const edgeClass: Record<EdgeState, string> = {
  ok: 'stroke-(--edge)',
  fail: 'stroke-(--edge-failed) [stroke-dasharray:7_5]',
  warn: 'stroke-warning [stroke-dasharray:7_5]',
  idle: 'stroke-(--edge-idle) [stroke-dasharray:3_5]',
}

export type Badge = { label: string; tone: Tone }

const sourceOn = (flow: Flow, id: number) => flow.sources.find((s) => s.id === id)?.enabled

// 推送规则的连线状态：规则、目的地、来源任何一个停用都算没连通。
export function bindingState(flow: Flow, b: FlowBinding): EdgeState {
  if (!b.enabled || !sourceOn(flow, b.source_id)) return 'idle'
  if (b.state === 'success') return 'ok'
  if (b.state === 'failed') return 'fail'
  if (b.state === 'retrying') return 'warn'
  return 'idle'
}

// 设备到来源的连线：手机连上过平台、来源也启用才算通。
export function coverState(flow: Flow, d: FlowDevice, sourceID: number): EdgeState {
  return d.status === 'connected' && sourceOn(flow, sourceID) ? 'ok' : 'idle'
}

const deviceStatus: Record<FlowDevice['status'], Badge> = {
  connected: { label: '已连通', tone: 'success' },
  installed: { label: '已安装', tone: 'info' },
  not_installed: { label: '未安装', tone: 'neutral' },
}

export function deviceBadges(d: FlowDevice): Badge[] {
  const out = [deviceStatus[d.status]]
  if (d.outdated) out.push({ label: '规则过时', tone: 'warning' })
  return out
}

export function sourceBadges(s: OverviewSource): Badge[] {
  const out: Badge[] = []
  if (!s.enabled) out.push({ label: '已停用', tone: 'neutral' })
  if (s.push_failed) out.push({ label: '推送失败', tone: 'danger' })
  if (s.pending_review) out.push({ label: `${s.pending_review} 个待审核`, tone: 'warning' })
  if (s.expired) out.push({ label: '已过期', tone: 'danger' })
  return out
}

export const isSoon = (v?: string | null) =>
  !!v && new Date(v).getTime() - Date.now() < 3 * 86400_000

export function sourceMeta(s: OverviewSource) {
  if (!s.enabled) return []
  return [
    { label: '身份', value: `${s.identities} 个` },
    {
      label: '最早过期',
      value: s.expires_at ? fmtUntil(s.expires_at) : '—',
      tone: isSoon(s.expires_at) ? ('warning' as const) : undefined,
    },
    { label: '最近回传', value: fmtAgo(s.last_seen_at) },
  ]
}

export const destStates: Record<string, Badge> = {
  success: { label: '正常', tone: 'success' },
  failed: { label: '失败', tone: 'danger' },
  retrying: { label: '等待重试', tone: 'warning' },
  queued: { label: '排队中', tone: 'info' },
  none: { label: '未推送', tone: 'neutral' },
  skipped: { label: '跳过', tone: 'neutral' },
}

// 目的地的徽章看推到它的各条连线：有失败算失败，其次等待重试；有一条推成功过就算正常，
// 不因为另一条还没推过就显示“还没推过”。
export function destBadges(flow: Flow, d: FlowDestination): Badge[] {
  if (!d.enabled) return [{ label: '已停用', tone: 'neutral' }]
  const states = flow.bindings
    .filter((b) => b.destination_id === d.id)
    .map((b) => bindingState(flow, b))
    .filter((s) => s !== 'idle')
  if (states.includes('fail')) return [destStates.failed!]
  if (states.includes('warn')) return [destStates.retrying!]
  if (states.includes('ok')) return [destStates.success!]
  return d.bindings ? [destStates.none!] : []
}

export function destLine(d: FlowDestination) {
  if (!d.bindings) return '暂无关联的推送规则'
  if (d.state === 'failed' || d.state === 'retrying') return d.last_error || '推送失败'
  return d.last_at ? `最近推送 ${fmtAgo(d.last_at)}` : '未推送'
}

export function sourceLine(flow: Flow, s: OverviewSource) {
  const bs = flow.bindings.filter((b) => b.source_id === s.id)
  if (!bs.length) return '暂无推送规则'
  const name = new Map(flow.destinations.map((d) => [d.id, d.name]))
  return (
    '推送至 ' +
    bs
      .map((b) => {
        const st = bindingState(flow, b)
        const tail = st === 'fail' ? '（失败）' : st === 'warn' ? '（等待重试）' : ''
        return name.get(b.destination_id) + tail
      })
      .join('、')
  )
}

// 聚焦的对象：类型和 ID，节点在页面上的标识是 dev-1、src-2、dst-3。
export type FocusKind = 'device' | 'source' | 'destination'
export const nodeKey = (kind: FocusKind, id: number) =>
  `${kind === 'device' ? 'dev' : kind === 'source' ? 'src' : 'dst'}-${id}`
