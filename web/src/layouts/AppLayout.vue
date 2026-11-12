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
import { crumbTail } from '@/lib/crumb'
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
  if (base !== '/' && route.path !== base) out.push({ label: crumbTail.value ?? '详情' })
  const last = out[out.length - 1]!
  if (route.path === last.to) delete last.to
  return out
})

// 导航角标：来源是待审核的值，订阅是待更新的，推送是推送异常。切换页面时刷新，另外每分钟刷新一次。
// 角标本身对读屏隐藏，label 以 sr-only 文字写进链接，读出来是「来源，1 个待审核」。
const overview = useOverview()
interface Badge {
  n: number
  label: string
  tone: string
  dot: string
}
function badgeOf(path: string): Badge | null {
  const a = overview.data?.attention
  if (!a) return null
  const warn = { tone: 'bg-warning/15 text-warning', dot: 'bg-warning' }
  if (path === '/sources' && a.pending_review)
    return { n: a.pending_review, label: `${a.pending_review} 个待审核`, ...warn }
  if (path === '/subscriptions' && a.outdated)
    return { n: a.outdated, label: `${a.outdated} 个待更新`, ...warn }
  const push = a.push_failed + a.push_retrying
  if (path === '/destinations' && push)
    return {
      n: push,
      label: `${push} 个推送异常`,
      tone: 'bg-destructive/12 text-destructive',
      dot: 'bg-destructive',
    }
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

// 跳到主要内容：焦点移到内容区，跳过侧栏和顶栏。不用 #锚点，免得改动地址栏。
function skipToMain() {
  document.getElementById('main-content')?.focus()
}

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
    <a
      href="#main-content"
      class="bg-background ring-ring/50 sr-only z-50 rounded-md border text-sm font-medium shadow-md ring-3 focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:px-3 focus:py-2"
      @click.prevent="skipToMain"
      >跳至主要内容</a
    >
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
                    <span
                      >{{ m.label
                      }}<span v-if="m.badge" class="sr-only">，{{ m.badge.label }}</span></span
                    >
                  </router-link>
                </SidebarMenuButton>
                <template v-if="m.badge">
                  <SidebarMenuBadge aria-hidden="true" :class="m.badge.tone">{{
                    m.badge.n
                  }}</SidebarMenuBadge>
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
        <div
          class="flex items-center justify-between gap-2 px-2 group-data-[collapsible=icon]:hidden"
        >
          <ThemeSwitch />
          <router-link
            v-if="auth.user?.version"
            to="/settings/system"
            class="text-muted-foreground hover:text-foreground truncate font-mono text-xs"
            title="系统信息"
            >{{
              /^\d/.test(auth.user.version) ? 'v' + auth.user.version : auth.user.version
            }}</router-link
          >
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
          <SearchIcon class="size-4" /> 搜索
          <kbd class="bg-muted rounded border px-1.5 font-mono text-xs">⌘K</kbd>
        </button>
        <button
          type="button"
          class="hover:bg-accent inline-flex size-10 items-center justify-center rounded-md border md:hidden"
          aria-label="搜索"
          @click="commandOpen = true"
        >
          <SearchIcon class="size-4" />
        </button>
      </header>
      <!-- 总览的画布铺满内容区，其余页面居中留边 -->
      <div
        id="main-content"
        tabindex="-1"
        class="outline-none"
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
        <template v-if="m.badge">
          <span
            aria-hidden="true"
            class="text-background absolute top-1.5 left-1/2 ml-1 flex h-4 min-w-4 items-center justify-center rounded-full px-1 text-[10px] leading-none font-semibold tabular-nums"
            :class="m.badge.dot"
            >{{ m.badge.n > 99 ? '99+' : m.badge.n }}</span
          >
          <span class="sr-only">，{{ m.badge.label }}</span>
        </template>
      </router-link>
    </nav>
  </SidebarProvider>
</template>
