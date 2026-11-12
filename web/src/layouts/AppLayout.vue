<script setup lang="ts">
import { ChevronsUpDownIcon, LogOutIcon, SearchIcon, UserRoundIcon } from '@lucide/vue'
import { computed, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { toast } from 'vue-sonner'

import { errorText } from '@/api'
import BrandMark from '@/components/BrandMark.vue'
import CommandMenu from '@/components/shell/CommandMenu.vue'
import StatusPill from '@/components/shell/StatusPill.vue'
import ThemeSwitch from '@/components/shell/ThemeSwitch.vue'
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
import { commandOpen } from '@/lib/command'
import { navFor, navItems, pageFor } from '@/lib/nav'
import { useAuth } from '@/stores/auth'
import { useOverview } from '@/stores/overview'

import CloseOnNavigate from './closeOnNavigate'

const auth = useAuth()
const route = useRoute()
const router = useRouter()

const current = computed(() => navFor(route.path))
const page = computed(() => pageFor(route.path))
// 面包屑：导航项 › 子页面 › 详情。
const crumbs = computed(() => {
  const out: { label: string; to?: string }[] = [
    { label: current.value.label, to: current.value.path },
  ]
  if (page.value && page.value.label !== current.value.label)
    out.push({ label: page.value.label, to: page.value.path })
  const base = page.value?.path ?? current.value.path
  if (base !== '/' && route.path !== base) out.push({ label: '详情' })
  const last = out[out.length - 1]!
  if (route.path === last.to) delete last.to
  return out
})

// 导航角标：来源是待审核的值，订阅是待更新的，推送是推送异常。切换页面时刷新，另外每分钟刷新一次。
const overview = useOverview()
function badgeOf(path: string): { n: number; tone: string; dot: string } | null {
  const a = overview.data?.attention
  if (!a) return null
  if (path === '/sources' && a.pending_review)
    return { n: a.pending_review, tone: 'bg-warning/15 text-warning', dot: 'bg-warning' }
  if (path === '/subscriptions' && a.outdated)
    return { n: a.outdated, tone: 'bg-warning/15 text-warning', dot: 'bg-warning' }
  const push = a.push_failed + a.push_retrying
  if (path === '/destinations' && push)
    return { n: push, tone: 'bg-destructive/12 text-destructive', dot: 'bg-destructive' }
  return null
}
const items = computed(() => navItems.map((m) => ({ ...m, badge: badgeOf(m.path) })))

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
    <CommandMenu />
    <!-- 宽屏可收窄成只显示图标（点顶栏按钮、拖侧栏边缘或按 Ctrl/⌘+B）；手机上用底部导航，不用侧栏 -->
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
        <SidebarGroup class="py-1">
          <SidebarGroupContent>
            <SidebarMenu class="gap-0.5">
              <SidebarMenuItem v-for="m in items" :key="m.path">
                <SidebarMenuButton
                  as-child
                  :is-active="current.path === m.path"
                  :tooltip="m.label"
                  class="h-9"
                >
                  <router-link :to="m.path">
                    <component
                      :is="m.icon"
                      :class="current.path === m.path ? 'text-primary' : 'text-muted-foreground'"
                    />
                    <span>{{ m.label }}</span>
                  </router-link>
                </SidebarMenuButton>
                <template v-if="m.badge">
                  <SidebarMenuBadge :class="m.badge.tone">{{ m.badge.n }}</SidebarMenuBadge>
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
      <SidebarFooter class="gap-2">
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton tooltip="跳转或执行（⌘K）" @click="commandOpen = true">
              <SearchIcon class="text-muted-foreground" />
              <span class="text-muted-foreground">搜索与命令</span>
              <kbd
                class="bg-muted text-muted-foreground ml-auto rounded border px-1.5 font-mono text-xs"
                >⌘K</kbd
              >
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
        <div class="px-2 group-data-[collapsible=icon]:hidden">
          <ThemeSwitch />
        </div>
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
        class="bg-background/85 sticky top-0 z-10 flex h-14 shrink-0 items-center gap-3 border-b px-4 backdrop-blur"
      >
        <router-link to="/" class="flex items-center gap-2 md:hidden" aria-label="总览">
          <BrandMark class="size-7" />
          <span class="font-semibold">Ferry</span>
        </router-link>
        <SidebarTrigger class="-ml-1 max-md:hidden" />
        <div class="bg-border h-4 w-px max-md:hidden" />
        <nav class="flex min-w-0 items-center gap-1.5 text-sm max-md:hidden" aria-label="当前位置">
          <template v-for="(c, i) in crumbs" :key="i">
            <span v-if="i" class="text-muted-foreground/60">/</span>
            <router-link
              v-if="c.to"
              :to="c.to"
              class="text-muted-foreground hover:text-foreground truncate"
              >{{ c.label }}</router-link
            >
            <span v-else class="truncate font-medium">{{ c.label }}</span>
          </template>
        </nav>
        <StatusPill class="max-md:ml-auto md:ml-2" />
        <button
          type="button"
          class="bg-card hover:bg-accent text-muted-foreground ml-auto hidden h-8 items-center gap-2 rounded-md border px-3 text-sm transition-colors md:inline-flex"
          @click="commandOpen = true"
        >
          <SearchIcon class="size-4" /> 跳转或执行
          <kbd class="bg-muted rounded border px-1.5 font-mono text-xs">⌘K</kbd>
        </button>
        <button
          type="button"
          class="hover:bg-accent inline-flex size-10 items-center justify-center rounded-md border md:hidden"
          aria-label="跳转或执行"
          @click="commandOpen = true"
        >
          <SearchIcon class="size-4" />
        </button>
      </header>
      <!-- 总览的画布铺满内容区，其余页面居中留边 -->
      <div
        :class="
          route.meta.fullBleed
            ? 'flex flex-1 flex-col'
            : 'mx-auto w-full max-w-6xl p-4 pb-24 md:p-6 lg:p-8'
        "
      >
        <router-view />
      </div>
    </SidebarInset>

    <!-- 手机底部导航 -->
    <nav
      class="bg-background/95 fixed inset-x-0 bottom-0 z-20 grid grid-cols-6 border-t pb-[env(safe-area-inset-bottom)] backdrop-blur md:hidden"
      aria-label="主导航"
    >
      <router-link
        v-for="m in items"
        :key="m.path"
        :to="m.path"
        class="relative flex min-h-14 flex-col items-center justify-center gap-1 text-xs"
        :class="current.path === m.path ? 'text-primary' : 'text-muted-foreground'"
        :aria-current="current.path === m.path ? 'page' : undefined"
      >
        <component :is="m.icon" class="size-5" />
        {{ m.short }}
        <span
          v-if="m.badge"
          aria-hidden="true"
          class="absolute top-2 left-1/2 ml-2 size-2 rounded-full"
          :class="m.badge.dot"
        />
      </router-link>
    </nav>
  </SidebarProvider>
</template>
