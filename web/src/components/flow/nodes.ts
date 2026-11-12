import { fmtAgo, fmtUntil } from '@/format'
import type { Tone } from '@/lib/tone'
import {
  type Flow,
  type FlowBinding,
  type FlowDestination,
  type FlowDevice,
  type OverviewSource,
  subscriptionStatus,
} from '@/types'

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

// 设备的安装状态和订阅列表用同一套说法。
export function deviceBadges(d: FlowDevice): Badge[] {
  const out: Badge[] = [subscriptionStatus[d.status]]
  if (d.outdated) out.push({ label: '规则过时', tone: 'warning' })
  return out
}

const stopped: Badge = { label: '已停用', tone: 'neutral' }

// 来源上要处理的事：推送失败、有值待审核、有值已过期。
export function sourceAlerts(s: OverviewSource): Badge[] {
  const out: Badge[] = []
  if (s.push_failed) out.push({ label: '推送失败', tone: 'danger' })
  if (s.pending_review) out.push({ label: `${s.pending_review} 个待审核`, tone: 'warning' })
  if (s.expired) out.push({ label: '已过期', tone: 'danger' })
  return out
}

export function sourceBadges(s: OverviewSource): Badge[] {
  return s.enabled ? sourceAlerts(s) : [stopped, ...sourceAlerts(s)]
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

// 一条推送规则自己的状态：规则或目的地停用时为已停用，否则是这条规则最近一次推送的结果。
// peerEnabled 是列表里这一行对象（来源或目的地）自己是否启用，停用时同样只写已停用。
// 来源详情的推送规则、目的地详情的来源列表、聚焦窄栏里两端的节点都用它，同一条规则在同一屏上只有一种说法。
export function bindingBadges(b: FlowBinding, peerEnabled = true): Badge[] {
  if (!b.enabled || !peerEnabled) return [stopped]
  return b.state ? [destStates[b.state]!] : []
}

// 目的地的徽章看推到它的各条连线：有失败算失败，其次等待重试；有一条推成功过就算正常，
// 不因为另一条还没推过就显示“还没推过”。
export function destBadges(flow: Flow, d: FlowDestination): Badge[] {
  if (!d.enabled) return [stopped]
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
