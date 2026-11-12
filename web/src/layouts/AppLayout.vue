<script setup lang="ts">
import { ChevronRightIcon, ChevronsUpDownIcon, LogOutIcon, UserRoundIcon } from '@lucide/vue'
import { computed, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { toast } from 'vue-sonner'

import { errorText } from '@/api'
import BrandMark from '@/components/BrandMark.vue'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
  SidebarRail,
  SidebarTrigger,
} from '@/components/ui/sidebar'
import { navFor, navGroups } from '@/lib/nav'
import { toneText } from '@/lib/tone'
import { useAuth } from '@/stores/auth'
import { useOverview } from '@/stores/overview'

import CloseOnNavigate from './closeOnNavigate'

const auth = useAuth()
const route = useRoute()
const router = useRouter()

const current = computed(() => navFor(route.path))
// 顶栏面包屑：分组 › 功能区，详情页再多一级。
const isDetail = computed(() => current.value.path !== '/' && route.path !== current.value.path)

// 侧栏角标：订阅是待更新的，登录态是待审核的值，推送是推送异常。切换页面时刷新，另外每分钟刷新一次。
const overview = useOverview()
function badgeOf(path: string): { n: number; badge: string; dot: string } | null {
  const a = overview.data?.attention
  if (!a) return null
  if (path === '/data' && a.pending_review)
    return { n: a.pending_review, badge: 'bg-warning/15 text-warning', dot: 'bg-warning' }
  if (path === '/subscriptions' && a.outdated)
    return { n: a.outdated, badge: 'bg-warning/15 text-warning', dot: 'bg-warning' }
  const push = a.push_failed + a.push_retrying
  if (path === '/destinations' && push)
    return { n: push, badge: 'bg-destructive/10 text-destructive', dot: 'bg-destructive' }
  return null
}
const groups = computed(() =>
  navGroups.map((g) => ({ ...g, items: g.items.map((m) => ({ ...m, badge: badgeOf(m.path) })) })),
)
let timer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  void overview.load()
  timer = setInterval(() => void overview.load(), 60_000)
})
onUnmounted(() => clearInterval(timer))
watch(
  () => route.path,
  () => void overview.load(),
)

async function logout() {
  try {
    await auth.logout()
    await router.push('/login')
  } catch (e) {
    toast.error(errorText(e))
  }
}
</script>

<template>
  <SidebarProvider>
    <CloseOnNavigate />
    <!-- 宽屏可收窄成只显示图标（点顶栏按钮、拖侧栏边缘或按 Ctrl/⌘+B），窄屏是抽屉 -->
    <Sidebar collapsible="icon">
      <SidebarHeader>
        <router-link
          to="/"
          class="flex items-center gap-2.5 px-2 py-1.5 group-data-[collapsible=icon]:px-0"
        >
          <BrandMark class="shrink-0" />
          <span class="font-semibold tracking-wide group-data-[collapsible=icon]:hidden"
            >Ferry</span
          >
        </router-link>
      </SidebarHeader>
      <SidebarContent>
        <SidebarGroup v-for="g in groups" :key="g.label" class="py-1">
          <SidebarGroupLabel v-if="g.label">{{ g.label }}</SidebarGroupLabel>
          <SidebarGroupContent>
            <SidebarMenu>
              <SidebarMenuItem v-for="m in g.items" :key="m.path">
                <SidebarMenuButton as-child :is-active="current.path === m.path" :tooltip="m.label">
                  <router-link :to="m.path">
                    <component
                      :is="m.icon"
                      :class="current.path === m.path ? toneText[m.tone] : 'text-muted-foreground'"
                    />
                    <span>{{ m.label }}</span>
                  </router-link>
                </SidebarMenuButton>
                <template v-if="m.badge">
                  <SidebarMenuBadge :class="m.badge.badge">{{ m.badge.n }}</SidebarMenuBadge>
                  <!-- 侧栏收窄成图标时角标隐藏，改在图标右上角显示一个点 -->
                  <span
                    aria-hidden="true"
                    class="pointer-events-none absolute top-1 right-1 hidden size-2 rounded-full group-data-[collapsible=icon]:block"
                    :class="m.badge.dot"
                  />
                </template>
              </SidebarMenuItem>
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>
      </SidebarContent>
      <SidebarFooter>
        <SidebarMenu>
          <SidebarMenuItem>
            <DropdownMenu>
              <DropdownMenuTrigger as-child>
                <SidebarMenuButton :tooltip="auth.user?.username">
                  <span
                    class="bg-muted text-muted-foreground flex size-6 shrink-0 items-center justify-center rounded-full text-xs font-semibold uppercase"
                    >{{ auth.user?.username?.slice(0, 1) }}</span
                  >
                  <span class="text-sm font-medium">{{ auth.user?.username }}</span>
                  <ChevronsUpDownIcon class="text-muted-foreground ml-auto" />
                </SidebarMenuButton>
              </DropdownMenuTrigger>
              <DropdownMenuContent side="top" align="start" class="w-48">
                <DropdownMenuItem @click="router.push('/account')">
                  <UserRoundIcon /> 账户
                </DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem variant="destructive" @click="logout"
                  ><LogOutIcon /> 退出登录</DropdownMenuItem
                >
              </DropdownMenuContent>
            </DropdownMenu>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
    <SidebarInset class="min-w-0">
      <header
        class="bg-background/85 sticky top-0 z-10 flex h-12 shrink-0 items-center gap-2 border-b px-4 backdrop-blur"
      >
        <SidebarTrigger class="-ml-1" />
        <div class="bg-border mr-1 h-4 w-px" />
        <nav class="flex min-w-0 items-center gap-1.5 text-sm" aria-label="当前位置">
          <template v-if="current.group">
            <span class="text-muted-foreground hidden sm:inline">{{ current.group }}</span>
            <ChevronRightIcon class="text-muted-foreground/60 hidden size-3.5 sm:inline" />
          </template>
          <router-link
            v-if="isDetail"
            :to="current.path"
            class="text-muted-foreground hover:text-foreground"
            >{{ current.label }}</router-link
          >
          <span v-else class="font-medium">{{ current.label }}</span>
          <template v-if="isDetail">
            <ChevronRightIcon class="text-muted-foreground/60 size-3.5" />
            <span class="font-medium">详情</span>
          </template>
        </nav>
      </header>
      <div class="mx-auto w-full max-w-6xl p-4 md:p-6 lg:p-8">
        <router-view />
      </div>
    </SidebarInset>
  </SidebarProvider>
</template>
