import axios from 'axios'
import { createRouter, createWebHistory } from 'vue-router'

import { useAuth } from '@/stores/auth'

// 总览和三类聚焦详情共用一个组件：在它们之间切换时组件不重建，节点卡能从全图平滑移到窄栏。
const FlowView = () => import('./views/FlowView.vue')
const flow = { component: FlowView, meta: { fullBleed: true } }

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/setup', component: () => import('./views/SetupView.vue'), meta: { public: true } },
    { path: '/login', component: () => import('./views/LoginView.vue'), meta: { public: true } },
    {
      path: '/',
      component: () => import('./layouts/AppLayout.vue'),
      children: [
        { path: '', ...flow },
        { path: 'sources/:id(\\d+)', ...flow },
        { path: 'subscriptions/:id(\\d+)', ...flow },
        { path: 'destinations/:id(\\d+)', ...flow },
        { path: 'sources', component: () => import('./views/sources/SourcesView.vue') },
        // 编辑器是列表页上的弹窗，地址仍可直接打开
        {
          path: 'sources/new',
          component: () => import('./views/sources/SourcesView.vue'),
          props: { editing: 'new' },
        },
        // 登录态页并进了来源详情：旧地址跳到来源列表，身份详情跳到所属来源并打开那个身份
        { path: 'data', redirect: '/sources' },
        {
          path: 'data/:id(\\d+)',
          component: FlowView,
          beforeEnter: async (to) => {
            try {
              const { data } = await axios.get<{ source_id: number }>(
                `/api/v1/identities/${to.params.id}`,
              )
              return { path: `/sources/${data.source_id}`, query: { identity: to.params.id } }
            } catch {
              return '/sources'
            }
          },
        },
        { path: 'subscriptions', component: () => import('./views/SubscriptionsView.vue') },
        { path: 'destinations', component: () => import('./views/DestinationsView.vue') },
        { path: 'notify', component: () => import('./views/NotifyView.vue') },
        { path: 'activity', component: () => import('./views/ActivityView.vue') },
        { path: 'settings/system', component: () => import('./views/SystemView.vue') },
        {
          path: 'settings',
          component: () => import('./views/SettingsView.vue'),
          // 审计日志搬到了记录页
          beforeEnter: (to) =>
            to.query.tab === 'audit'
              ? { path: '/activity', query: { tab: 'audit', category: to.query.category } }
              : true,
        },
        { path: 'account', component: () => import('./views/AccountView.vue') },
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})

let setupChecked = false

router.beforeEach(async (to) => {
  // 每次打开页面先看是否需要初始化，向导完成前其他页面都进不去。
  if (!setupChecked) {
    try {
      const { data } = await axios.get<{ needs_config: boolean; needs_admin: boolean }>(
        '/api/v1/setup/status',
      )
      setupChecked = true
      if ((data.needs_config || data.needs_admin) && to.path !== '/setup') return '/setup'
    } catch {
      // 服务不可用时照常进入，页面上的请求会给出提示
    }
  }
  if (to.meta.public) return true
  const auth = useAuth()
  if (!auth.checked) await auth.load()
  if (!auth.user) return { path: '/login', query: { next: to.fullPath } }
  return true
})

export function markSetupDone() {
  setupChecked = true
}

export default router
