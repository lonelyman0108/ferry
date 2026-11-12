import {
  BellRingIcon,
  BoxesIcon,
  DatabaseIcon,
  LayoutDashboardIcon,
  QrCodeIcon,
  SendIcon,
  SettingsIcon,
  UserRoundIcon,
} from '@lucide/vue'
import type { Component } from 'vue'

import type { Tone } from './tone'

export interface NavItem {
  path: string
  label: string
  icon: Component
  tone: Tone
  // 页面标题下的一句话说明
  description: string
}

// 侧边栏按数据流向分组：接入 → 数据 → 分发 → 系统。
export const navGroups: { label: string; items: NavItem[] }[] = [
  {
    label: '',
    items: [
      {
        path: '/',
        label: '总览',
        icon: LayoutDashboardIcon,
        tone: 'primary',
        description: '需要处理的事和各来源的状态',
      },
    ],
  },
  {
    label: '接入',
    items: [
      {
        path: '/sources',
        label: '来源',
        icon: BoxesIcon,
        tone: 'violet',
        description: '请求匹配、字段提取与输出变量',
      },
      {
        path: '/subscriptions',
        label: '订阅',
        icon: QrCodeIcon,
        tone: 'indigo',
        description: '代理客户端插件与安装链接',
      },
    ],
  },
  {
    label: '数据',
    items: [
      {
        path: '/data',
        label: '登录态',
        icon: DatabaseIcon,
        tone: 'sky',
        description: '收集中的回传，各身份的当前值与历史值',
      },
    ],
  },
  {
    label: '分发',
    items: [
      {
        path: '/destinations',
        label: '推送',
        icon: SendIcon,
        tone: 'emerald',
        description: '目的地、推送规则与推送记录',
      },
      {
        path: '/notify',
        label: '通知',
        icon: BellRingIcon,
        tone: 'amber',
        description: '通知渠道与通知规则',
      },
    ],
  },
  {
    label: '系统',
    items: [
      {
        path: '/settings',
        label: '系统设置',
        icon: SettingsIcon,
        tone: 'slate',
        description: '访问地址与审计日志',
      },
    ],
  },
]

// 不在侧边栏里、从左下角用户菜单进入的页面。
const userItems: NavItem[] = [
  {
    path: '/account',
    label: '账户',
    icon: UserRoundIcon,
    tone: 'slate',
    description: '登录密码与 API 令牌',
  },
]

export const navItems = [
  ...navGroups.flatMap((g) => g.items.map((i) => ({ ...i, group: g.label }))),
  ...userItems.map((i) => ({ ...i, group: '' })),
]

// 详情页（如 /sources/3）也归到对应的一级菜单。
export function navFor(path: string) {
  return (
    navItems.find((m) => m.path !== '/' && path.startsWith(m.path)) ??
    navItems.find((m) => m.path === '/')!
  )
}
