import {
  ActivityIcon,
  BoxesIcon,
  LayoutDashboardIcon,
  SendIcon,
  SettingsIcon,
  SmartphoneIcon,
} from '@lucide/vue'
import type { Component } from 'vue'

import type { Tone } from './tone'

// 一个导航项下面可以有几页，页面标题下用一排链接切换。
export interface SubPage {
  path: string
  label: string
  description: string
}

export interface NavItem {
  path: string
  label: string
  // 手机底部导航用的短名
  short: string
  icon: Component
  tone: Tone
  // 页面标题下的一句话说明
  description: string
  pages?: SubPage[]
}

// 导航按一条数据走的路排：总览看全图，来源和订阅是入口，推送是出口，记录是经过。
export const navItems: NavItem[] = [
  {
    path: '/',
    label: '总览',
    short: '总览',
    icon: LayoutDashboardIcon,
    tone: 'primary',
    description: '要处理的事和各来源的状态',
  },
  {
    path: '/sources',
    label: '来源',
    short: '来源',
    icon: BoxesIcon,
    tone: 'primary',
    description: '读取规则、身份与登录态',
  },
  {
    path: '/subscriptions',
    label: '订阅',
    short: '订阅',
    icon: SmartphoneIcon,
    tone: 'primary',
    description: '装在手机代理客户端里的插件与安装链接',
  },
  {
    path: '/destinations',
    label: '推送',
    short: '推送',
    icon: SendIcon,
    tone: 'primary',
    description: '目的地、推送规则与通知',
    pages: [
      { path: '/destinations', label: '目的地', description: '目的地、推送规则与推送记录' },
      { path: '/notify', label: '通知', description: '通知渠道与通知规则' },
    ],
  },
  {
    path: '/activity',
    label: '记录',
    short: '记录',
    icon: ActivityIcon,
    tone: 'primary',
    description: '每一次回传、审核、推送与通知',
  },
  {
    path: '/settings',
    label: '设置',
    short: '设置',
    icon: SettingsIcon,
    tone: 'primary',
    description: '访问地址与账户',
    pages: [
      { path: '/settings', label: '访问地址', description: '手机拉取规则、回传登录态用的地址' },
      { path: '/account', label: '账户', description: '登录密码与 API 令牌' },
    ],
  },
]

function owns(item: NavItem, path: string) {
  const roots = item.pages?.map((p) => p.path) ?? [item.path]
  return roots.some((r) => (r === '/' ? path === '/' : path === r || path.startsWith(r + '/')))
}

// 当前地址属于哪个导航项；详情页（如 /sources/3）也归到对应的导航项。
export function navFor(path: string): NavItem {
  return navItems.find((m) => m.path !== '/' && owns(m, path)) ?? navItems[0]!
}

// 当前地址是导航项下的哪一页。
export function pageFor(path: string): SubPage | undefined {
  return navFor(path).pages?.find((p) => path === p.path || path.startsWith(p.path + '/'))
}
