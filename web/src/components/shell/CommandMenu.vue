<script setup lang="ts">
import { MoonIcon, PlusIcon, RefreshCwIcon, SunIcon } from '@lucide/vue'
import { onKeyStroke } from '@vueuse/core'
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'

import { api } from '@/api'
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import EntityIcon from '@/components/EntityIcon.vue'
import { useInboxActions } from '@/components/flow/inbox'
import SourceAvatar from '@/components/SourceAvatar.vue'
import { commandOpen } from '@/lib/command'
import { channelMark, destMark, deviceMark } from '@/lib/icons'
import { navItems } from '@/lib/nav'
import { toggleTheme } from '@/lib/theme'
import { useFlow } from '@/stores/flow'
import { useOverview } from '@/stores/overview'
import { useSourceIcons } from '@/stores/sourceIcons'
import {
  type Channel,
  clients,
  type Destination,
  type SourceSummary,
  type Subscription,
} from '@/types'

const router = useRouter()
const overview = useOverview()
const flow = useFlow()
const { retryAll } = useInboxActions()

// 只重试每个“推送规则 + 身份”最近一次失败的任务，已经被后来的成功覆盖的旧任务不算。
const failed = computed(() => overview.data?.items.filter((it) => it.kind === 'push_failed') ?? [])

async function retryFailed() {
  commandOpen.value = false
  await retryAll(failed.value)
  await Promise.all([overview.load(), flow.load().catch(() => {})])
}

// 按 ⌘K / Ctrl+K 打开；输入框里也能用，所以不检查焦点在哪。
onKeyStroke('k', (e) => {
  if (!(e.metaKey || e.ctrlKey)) return
  e.preventDefault()
  commandOpen.value = !commandOpen.value
})

const sources = ref<SourceSummary[]>([])
const subs = ref<Subscription[]>([])
const dests = ref<Destination[]>([])
const channels = ref<Channel[]>([])

// 每次打开时取一次对象列表；取不到就只剩页面和操作，不打扰。
watch(commandOpen, async (open) => {
  if (!open) return
  const [a, b, c, d] = await Promise.allSettled([
    api.get<SourceSummary[]>('/sources'),
    api.get<Subscription[]>('/subscriptions'),
    api.get<Destination[]>('/destinations'),
    api.get<Channel[]>('/channels'),
  ])
  if (a.status === 'fulfilled') {
    sources.value = a.value.data
    useSourceIcons().set(a.value.data)
  }
  if (b.status === 'fulfilled') subs.value = b.value.data
  if (c.status === 'fulfilled') dests.value = c.value.data
  if (d.status === 'fulfilled') channels.value = d.value.data
})

const clientName = (v: string) => clients.find((c) => c.value === v)?.label ?? v

function go(to: string) {
  commandOpen.value = false
  void router.push(to)
}

function run(fn: () => void) {
  commandOpen.value = false
  fn()
}
</script>

<template>
  <CommandDialog
    v-model:open="commandOpen"
    class="sm:max-w-xl"
    title="搜索与命令"
    description="搜索页面、对象或操作"
  >
    <CommandInput placeholder="搜索页面、来源、订阅、目的地或操作…" />
    <CommandList class="max-h-[min(60vh,420px)]">
      <CommandEmpty>无匹配结果</CommandEmpty>
      <CommandGroup heading="操作">
        <CommandItem value="新建来源" @select="go('/sources/new')"
          ><PlusIcon /> 新建来源</CommandItem
        >
        <CommandItem v-if="failed.length" value="重试全部失败推送" @select="retryFailed"
          ><RefreshCwIcon /> 重试全部失败推送（{{ failed.length }} 条）</CommandItem
        >
        <CommandItem value="切换深浅色 主题" @select="run(toggleTheme)"
          ><SunIcon class="dark:hidden" /><MoonIcon class="hidden dark:block" />
          切换深浅色</CommandItem
        >
      </CommandGroup>
      <CommandGroup heading="页面">
        <template v-for="m in navItems" :key="m.path">
          <CommandItem
            v-for="p in m.pages ?? [{ path: m.path, label: m.label, icon: m.icon }]"
            :key="p.path"
            :value="`页面 ${m.label} ${p.label}`"
            @select="go(p.path)"
          >
            <component :is="p.icon ?? m.icon" />
            {{ p.label === m.label ? m.label : `${m.label} · ${p.label}` }}
          </CommandItem>
        </template>
      </CommandGroup>
      <CommandGroup v-if="sources.length" heading="来源">
        <CommandItem
          v-for="s in sources"
          :key="s.id"
          :value="`来源 ${s.name} ${s.key}`"
          @select="go(`/sources/${s.id}`)"
        >
          <SourceAvatar :name="s.name" :skey="s.key" :icon="s.icon ?? ''" size="xs" />
          {{ s.name }}
          <span class="text-muted-foreground ml-auto font-mono text-xs">{{ s.key }}</span>
        </CommandItem>
      </CommandGroup>
      <CommandGroup v-if="subs.length" heading="订阅">
        <CommandItem
          v-for="s in subs"
          :key="s.id"
          :value="`订阅 ${s.name} ${clientName(s.client)}`"
          @select="go(`/subscriptions/${s.id}`)"
        >
          <EntityIcon :mark="deviceMark(s.client)" size="xs" /> {{ s.name }}
          <span class="text-muted-foreground ml-auto text-xs">{{ clientName(s.client) }}</span>
        </CommandItem>
      </CommandGroup>
      <CommandGroup v-if="dests.length" heading="目的地">
        <CommandItem
          v-for="d in dests"
          :key="d.id"
          :value="`目的地 ${d.name}`"
          @select="go(`/destinations/${d.id}`)"
        >
          <EntityIcon :mark="destMark(d.kind)" size="xs" /> {{ d.name }}
        </CommandItem>
      </CommandGroup>
      <CommandGroup v-if="channels.length" heading="通知渠道">
        <CommandItem
          v-for="c in channels"
          :key="c.id"
          :value="`通知渠道 ${c.name}`"
          @select="go('/notify')"
        >
          <EntityIcon :mark="channelMark(c.kind)" size="xs" /> {{ c.name }}
        </CommandItem>
      </CommandGroup>
    </CommandList>
  </CommandDialog>
</template>
