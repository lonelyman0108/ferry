import axios from 'axios'
import { createRouter, createWebHistory } from 'vue-router'

import { useAuth } from '@/stores/auth'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/setup', component: () => import('./views/SetupView.vue'), meta: { public: true } },
    { path: '/login', component: () => import('./views/LoginView.vue'), meta: { public: true } },
    {
      path: '/',
      component: () => import('./layouts/AppLayout.vue'),
      children: [
        { path: '', component: () => import('./views/OverviewView.vue') },
        { path: 'sources', component: () => import('./views/sources/SourcesView.vue') },
        // 编辑器是列表页上的弹窗，地址仍可直接打开
        {
          path: 'sources/new',
          component: () => import('./views/sources/SourcesView.vue'),
          props: { editing: 'new' },
        },
        {
          path: 'sources/:id(\\d+)',
          component: () => import('./views/sources/SourcesView.vue'),
          props: (r) => ({ editing: r.params.id }),
        },
        { path: 'data', component: () => import('./views/data/DataView.vue') },
        // 详情是列表页上的弹窗，地址仍可直接打开
        {
          path: 'data/:id(\\d+)',
          component: () => import('./views/data/DataView.vue'),
          props: (r) => ({ viewing: r.params.id }),
        },
        { path: 'subscriptions', component: () => import('./views/SubscriptionsView.vue') },
        { path: 'destinations', component: () => import('./views/DestinationsView.vue') },
        { path: 'notify', component: () => import('./views/NotifyView.vue') },
        { path: 'activity', component: () => import('./views/ActivityView.vue') },
        {
          path: 'settings',
          component: () => import('./views/SettingsView.vue'),
          // 审计日志搬到了记录页
          beforeEnter: (to) =>
            to.query.tab === 'audit'
              ? { path: '/activity', query: { category: to.query.category } }
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
