import { ActivityIcon, LayoutDashboardIcon, SettingsIcon } from '@lucide/vue'
import type { Component } from 'vue'

import { icons } from './icons'

// 一个导航项下面可以有几页，页面标题下用一排链接切换。
export interface SubPage {
  path: string
  label: string
  description: string
  // 命令面板里的图标，不给时用所属导航项的
  icon?: Component
  // 命令面板里额外的检索词，不显示：页内标签页的名称，说明里已经有的不必重复
  keywords?: string
}

export interface NavItem {
  path: string
  label: string
  // 手机底部导航用的短名
  short: string
  icon: Component
  // 页面标题下的一句话说明
  description: string
  // 同 SubPage.keywords
  keywords?: string
  pages?: SubPage[]
}

// 导航按一条数据走的路排：总览看全图，来源和订阅是入口，推送是出口，记录是经过。
export const navItems: NavItem[] = [
  {
    path: '/',
    label: '总览',
    short: '总览',
    icon: LayoutDashboardIcon,
    description: '待处理事项与各来源状态',
  },
  {
    path: '/sources',
    label: '来源',
    short: '来源',
    icon: icons.source,
    description: '读取规则、身份与登录态',
  },
  {
    path: '/subscriptions',
    label: '订阅',
    short: '订阅',
    icon: icons.subscription,
    description: '代理客户端中安装的插件与安装链接',
  },
  {
    path: '/destinations',
    label: '推送',
    short: '推送',
    icon: icons.push,
    description: '目的地、推送规则与通知',
    pages: [
      {
        path: '/destinations',
        label: '目的地',
        description: '目的地、推送规则与推送记录',
        icon: icons.destination,
      },
      { path: '/notify', label: '通知', description: '通知渠道与通知规则', icon: icons.notify },
    ],
  },
  {
    path: '/activity',
    label: '记录',
    short: '记录',
    icon: ActivityIcon,
    description: '回传、审核、推送与通知的完整记录',
    keywords: '时间线 审计日志',
  },
  {
    path: '/settings',
    label: '设置',
    short: '设置',
    icon: SettingsIcon,
    description: '访问地址与账户',
    pages: [
      {
        path: '/settings',
        label: '访问地址',
        description: '设备拉取读取规则、回传登录态所用的地址',
      },
      {
        path: '/account',
        label: '账户',
        description: '登录密码、API 令牌与深浅色',
        keywords: '修改密码 外观 API令牌 令牌',
      },
      {
        path: '/settings/system',
        label: '系统信息',
        description: '版本、运行环境、数据库与配置来源',
      },
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
  // 子页地址可能互为前缀（/settings 与 /settings/system），取最长的那个。
  return navFor(path)
    .pages?.filter((p) => path === p.path || path.startsWith(p.path + '/'))
    .sort((a, b) => b.path.length - a.path.length)[0]
}
