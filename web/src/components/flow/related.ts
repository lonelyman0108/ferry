import { BoxesIcon, SendIcon, SmartphoneIcon, WebhookIcon } from '@lucide/vue'
import { type Component, computed } from 'vue'

import type { Flow } from '@/types'

import { clientName } from './inbox'
import {
  type Badge,
  bindingState,
  coverState,
  destBadges,
  deviceBadges,
  type EdgeState,
  type FocusKind,
  nodeKey,
  sourceBadges,
} from './nodes'

// 聚焦时和当前对象直接相关的节点：来源的上游是覆盖它的订阅、下游是它推到的目的地；
// 设备只有下游（它覆盖的来源）；目的地只有上游（推到它的来源）。窄栏和手机上的横向链路共用。
export interface RailNode {
  key: string
  kind: FocusKind
  id: number
  icon: Component
  title: string
  subtitle: string
  mono?: boolean
  badges: Badge[]
  muted: boolean
  edge?: EdgeState
}

export function useRelated(flow: () => Flow, kind: () => FocusKind, id: () => number) {
  function deviceNode(id: number, edge?: EdgeState): RailNode | null {
    const d = flow().devices.find((x) => x.id === id)
    if (!d) return null
    return {
      key: nodeKey('device', id),
      kind: 'device',
      id,
      icon: SmartphoneIcon,
      title: d.name,
      subtitle: clientName(d.client),
      badges: deviceBadges(d),
      muted: d.status === 'not_installed',
      edge,
    }
  }

  function sourceNode(id: number, edge?: EdgeState): RailNode | null {
    const s = flow().sources.find((x) => x.id === id)
    if (!s) return null
    return {
      key: nodeKey('source', id),
      kind: 'source',
      id,
      icon: BoxesIcon,
      title: s.name,
      subtitle: s.key,
      mono: true,
      badges: sourceBadges(s),
      muted: !s.enabled,
      edge,
    }
  }

  function destNode(id: number, edge?: EdgeState): RailNode | null {
    const d = flow().destinations.find((x) => x.id === id)
    if (!d) return null
    return {
      key: nodeKey('destination', id),
      kind: 'destination',
      id,
      icon: d.kind === 'webhook' ? WebhookIcon : SendIcon,
      title: d.name,
      subtitle: d.kind === 'webhook' ? 'Webhook' : '青龙',
      badges: destBadges(flow(), d),
      muted: !d.enabled,
      edge,
    }
  }

  const nonNull = <T>(xs: (T | null)[]) => xs.filter((x): x is T => x !== null)

  const current = computed(() =>
    kind() === 'device'
      ? deviceNode(id())
      : kind() === 'source'
        ? sourceNode(id())
        : destNode(id()),
  )

  // 上游与下游，各带一条连向当前节点的线的状态。
  const groups = computed(() => {
    const f = flow()
    if (kind() === 'source') {
      const up = f.devices
        .filter((d) => d.source_ids.includes(id()))
        .map((d) => deviceNode(d.id, coverState(f, d, id())))
      const down = f.bindings
        .filter((b) => b.source_id === id())
        .map((b) => destNode(b.destination_id, bindingState(f, b)))
      return {
        up: nonNull(up),
        upLabel: '覆盖此来源的订阅',
        down: nonNull(down),
        downLabel: '推送至',
      }
    }
    if (kind() === 'device') {
      const d = f.devices.find((x) => x.id === id())
      const down = (d?.source_ids ?? []).map((sid) =>
        sourceNode(sid, d ? coverState(f, d, sid) : 'idle'),
      )
      return { up: [], upLabel: '', down: nonNull(down), downLabel: '覆盖的来源' }
    }
    const up = f.bindings
      .filter((b) => b.destination_id === id())
      .map((b) => sourceNode(b.source_id, bindingState(f, b)))
    return { up: nonNull(up), upLabel: '推送至此的来源', down: [], downLabel: '' }
  })

  const hidden = computed(() => {
    const f = flow()
    const shown = 1 + groups.value.up.length + groups.value.down.length
    return f.devices.length + f.sources.length + f.destinations.length - shown
  })

  return { current, groups, hidden }
}
