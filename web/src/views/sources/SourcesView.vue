<script setup lang="ts">
import {
  ChevronRightIcon,
  ExternalLinkIcon,
  FileUpIcon,
  KeyRoundIcon,
  PackageSearchIcon,
  PlusIcon,
  RouteIcon,
  VariableIcon,
} from '@lucide/vue'
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { toast } from 'vue-sonner'

import { api, errorText } from '@/api'
import EmptyState from '@/components/EmptyState.vue'
import { type Badge, sourceAlerts } from '@/components/flow/nodes'
import ListSkeleton from '@/components/ListSkeleton.vue'
import SourceEditor from './SourceEditor.vue'
import SourceImportDialog from './SourceImportDialog.vue'
import PageHeader from '@/components/PageHeader.vue'
import RowActions from '@/components/RowActions.vue'
import SourceAvatar from '@/components/SourceAvatar.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { Button } from '@/components/ui/button'
import { fmtAgo } from '@/format'
import { icons } from '@/lib/icons'
import { useOverview } from '@/stores/overview'
import { useSourceIcons } from '@/stores/sourceIcons'
import type { SourceSummary, SourceView, Subscription } from '@/types'

const props = defineProps<{ editing?: string; importing?: boolean }>()
const router = useRouter()
const route = useRoute()
const list = ref<SourceSummary[] | null>(null)
const showImport = ref(false)

function openImport() {
  showImport.value = true
}

function closeImport() {
  showImport.value = false
  if (props.importing) {
    void router.replace('/sources')
  }
}

async function onImported(singleId?: number) {
  await load()
  if (singleId) {
    await router.push({ path: `/sources/${singleId}`, query: { edit: 'basic' } })
  } else if (props.importing) {
    await router.replace('/sources')
  }
}

// 卡片上的接入状态：读取它的订阅数来自订阅列表，推送规则数和最近回传来自总览。
const overview = useOverview()
const subs = ref<Subscription[]>([])
const subsOf = (s: SourceSummary) =>
  subs.value.filter((x) => x.all_sources || x.source_keys.includes(s.key)).length
const statOf = (s: SourceSummary) => overview.data?.sources.find((x) => x.id === s.id)
// 要处理的事（推送失败、待审核、已过期）和流向图节点同一口径；卡片上没有过期时间，即将过期也单独标出。
// 侧栏“来源”的角标数的就是这些，点卡片进详情，详情顶部列出具体是哪个身份的哪个值。
function alertsOf(s: SourceSummary): Badge[] {
  const st = statOf(s)
  if (!st) return []
  const out = sourceAlerts(st)
  if (st.expiring && !st.expired) out.push({ label: '即将过期', tone: 'warning' })
  return out
}

async function load() {
  try {
    const [l, sb] = await Promise.all([
      api.get<SourceSummary[]>('/sources'),
      api.get<Subscription[]>('/subscriptions'),
      overview.load(),
    ])
    list.value = l.data
    useSourceIcons().set(l.data)
    subs.value = sb.data
  } catch (e) {
    toast.error(errorText(e))
  }
}
onMounted(load)

// 新建的来源保存后换成它的地址，编辑器随之按新 id 重新加载。
async function onSaved(id: number) {
  await load()
  // 新建的来源保存后进入它的详情，并打开接入清单。
  if (props.editing === 'new' && id)
    await router.replace({ path: `/sources/${id}`, query: { edit: 'setup' } })
}

// 卡片上的启用开关：来源按整份文档读写，先取完整定义再改 enabled 保存。
const toggling = ref(0)
async function toggle(s: SourceSummary, enabled: boolean) {
  toggling.value = s.id
  try {
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    const { id, updated_at, ...doc } = (await api.get<SourceView>(`/sources/${s.id}`)).data
    await api.put(`/sources/${s.id}`, { ...doc, enabled })
    s.enabled = enabled
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    toggling.value = 0
  }
}
</script>

<template>
  <PageHeader>
    <template #actions>
      <Button variant="outline" @click="router.push('/sources/hub')"
        ><PackageSearchIcon /> 浏览仓库</Button
      >
      <Button variant="outline" @click="openImport"><FileUpIcon /> 导入</Button>
      <Button @click="router.push('/sources/new')"><PlusIcon /> 新建来源</Button>
    </template>
  </PageHeader>

  <ListSkeleton v-if="!list" :rows="3" class="md:grid-cols-2 xl:grid-cols-3" item-class="h-40" />
  <EmptyState
    v-else-if="!list.length"
    :icon="icons.source"
    title="暂无来源"
    description="来源定义目标 App 的请求匹配、字段读取规则与输出变量。可从来源仓库浏览并导入。"
  >
    <Button @click="router.push('/sources/hub')"><PackageSearchIcon /> 浏览仓库</Button>
    <Button variant="outline" @click="router.push('/sources/new')"><PlusIcon /> 新建来源</Button>
  </EmptyState>

  <!-- 整张卡可点：标题链接的 ::after 铺满卡片，开关和「接入清单」按钮叠在它上面（relative z-10），
       避免把交互元素套在另一个可点元素里 -->
  <div v-else-if="list" class="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
    <div
      v-for="s in list"
      :key="s.id"
      class="bg-card ring-foreground/10 hover:ring-primary/40 has-[[data-card-link]:focus-visible]:ring-ring/50 relative flex flex-col gap-4 rounded-xl p-4 text-left ring-1 transition hover:shadow-sm has-[[data-card-link]:focus-visible]:ring-3"
    >
      <div class="flex items-start gap-3">
        <SourceAvatar :name="s.name" :skey="s.key" :icon="s.icon ?? ''" :disabled="!s.enabled" />
        <div class="min-w-0 flex-1">
          <div class="flex min-w-0 flex-wrap items-center gap-2">
            <router-link
              :to="`/sources/${s.id}`"
              data-card-link
              class="truncate font-semibold outline-none after:absolute after:inset-0 after:rounded-xl"
              >{{ s.name }}</router-link
            >
            <StatusBadge v-if="!s.enabled" dot>已停用</StatusBadge>
            <StatusBadge v-if="s.origin?.latest_version" tone="warning" dot
              >有更新 v{{ s.origin.latest_version }}</StatusBadge
            >
          </div>
          <div class="text-muted-foreground flex flex-wrap items-center gap-x-2 text-xs">
            <span class="truncate font-mono">{{ s.key }}</span>
            <span v-if="s.origin?.version" class="font-mono">v{{ s.origin.version }}</span>
            <span v-if="s.origin?.repo_name">· {{ s.origin.repo_name }}</span>
            <span v-if="s.origin?.local_modified" class="text-warning">· 已修改</span>
            <a
              v-if="s.origin?.readme_url"
              :href="s.origin.readme_url"
              target="_blank"
              rel="noopener"
              class="text-primary relative z-10 inline-flex items-center gap-0.5 hover:underline"
              @click.stop
              ><ExternalLinkIcon class="size-3" />说明</a
            >
          </div>
          <div v-if="alertsOf(s).length" class="mt-2 flex flex-wrap gap-1.5">
            <StatusBadge v-for="b in alertsOf(s)" :key="b.label" :tone="b.tone" dot>{{
              b.label
            }}</StatusBadge>
          </div>
        </div>
        <RowActions
          :name="s.name"
          :enabled="s.enabled"
          :editable="false"
          :toggling="toggling === s.id"
          class="relative z-10"
          @toggle="(v) => toggle(s, v)"
        />
      </div>
      <div class="text-muted-foreground flex flex-wrap gap-x-4 gap-y-1 text-xs">
        <span class="flex items-center gap-1"
          ><RouteIcon class="size-3.5" />{{ s.matchers }} 条匹配规则</span
        >
        <span class="flex items-center gap-1"
          ><KeyRoundIcon class="size-3.5" />{{ s.fields }} 个字段</span
        >
        <span class="flex items-center gap-1"
          ><VariableIcon class="size-3.5" />{{ s.outputs }} 个输出变量</span
        >
      </div>
      <!-- 接入状态：没加入订阅、没有推送规则的标黄，点“接入清单”直接去补 -->
      <div class="mt-auto flex flex-wrap items-center gap-x-3 gap-y-1 border-t pt-3 text-xs">
        <span :class="subsOf(s) ? 'text-muted-foreground' : 'text-warning'">{{
          subsOf(s) ? `${subsOf(s)} 个订阅` : '未加入订阅'
        }}</span>
        <span :class="statOf(s)?.bindings ? 'text-muted-foreground' : 'text-warning'">{{
          statOf(s)?.bindings ? `${statOf(s)?.bindings} 条推送规则` : '无推送规则'
        }}</span>
        <span class="text-muted-foreground">{{
          statOf(s)?.last_seen_at ? `最近回传 ${fmtAgo(statOf(s)?.last_seen_at)}` : '未回传'
        }}</span>
        <button
          type="button"
          class="text-primary focus-visible:ring-ring/50 relative z-10 ml-auto flex items-center gap-0.5 rounded-sm outline-none hover:underline focus-visible:ring-3 max-md:-my-3.5 max-md:-mr-2 max-md:min-h-11 max-md:px-2"
          @click.stop="router.push({ path: `/sources/${s.id}`, query: { edit: 'setup' } })"
        >
          接入清单<span class="sr-only">「{{ s.name }}」</span><ChevronRightIcon class="size-3.5" />
        </button>
      </div>
    </div>
  </div>

  <SourceImportDialog
    :open="showImport || !!importing"
    :initial-url="typeof route.query.url === 'string' ? route.query.url : undefined"
    @close="closeImport"
    @imported="onImported"
  />

  <SourceEditor
    v-if="editing"
    :id="editing === 'new' ? undefined : editing"
    :key="editing"
    :initial-section="typeof route.query.section === 'string' ? route.query.section : undefined"
    @close="router.push('/sources')"
    @saved="onSaved"
  />
</template>
