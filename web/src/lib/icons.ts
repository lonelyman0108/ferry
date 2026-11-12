import {
  BellIcon,
  BellRingIcon,
  BoxesIcon,
  ClipboardCheckIcon,
  ListChecksIcon,
  RotateCcwKeyIcon,
  SendIcon,
  ServerIcon,
  SmartphoneIcon,
  WaypointsIcon,
  WebhookIcon,
} from '@lucide/vue'
import type { Component } from 'vue'

import { brands } from './brands'

// 概念与图标的对照，各页面统一从这里取，同一个概念在哪儿都是同一个图标。
// 具体的来源、订阅、目的地、通知渠道用各自的标识（见下面的 *Mark），这里的图标只用在导航、分组标题、标签页、空状态上。
export const icons = {
  source: BoxesIcon,
  subscription: SmartphoneIcon,
  destination: ServerIcon,
  // 推送这个动作：「推送」导航、手动推送、立即推送、记录里的推送事件
  push: SendIcon,
  bindings: WaypointsIcon,
  notify: BellIcon,
  channel: BellRingIcon,
  notifyRules: ListChecksIcon,
  checklist: ClipboardCheckIcon,
  rotateToken: RotateCcwKeyIcon,
  webhook: WebhookIcon,
} satisfies Record<string, Component>

// 一个对象在列表、画布、详情头部显示的标识：品牌图标、来源头像，或中性色图标块。
// 来源带上 enabled，停用的来源头像显示为灰色。
export type Mark =
  | { brand: string }
  | { source: { key: string; name: string; icon?: string; enabled?: boolean } }
  | { icon: Component }

// 订阅按客户端显示品牌图标，未知客户端退回手机图标。
export const deviceMark = (client: string): Mark =>
  brands[client] ? { brand: client } : { icon: icons.subscription }

// 目的地：青龙用品牌图标，Webhook 用中性色的 Webhook 图标。
export const destMark = (kind: string): Mark =>
  kind === 'qinglong' ? { brand: 'qinglong' } : { icon: icons.webhook }

// 通知渠道：Bark、Telegram 用品牌图标，Webhook 同目的地。
export const channelMark = (kind: string): Mark =>
  brands[kind] ? { brand: kind } : { icon: icons.webhook }

export const sourceMark = (s: {
  key: string
  name: string
  icon?: string
  enabled?: boolean
}): Mark => ({
  source: s,
})
