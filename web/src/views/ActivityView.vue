<script setup lang="ts">
import { ActivityIcon, ScrollTextIcon } from '@lucide/vue'
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { toast } from 'vue-sonner'

import { api, errorText } from '@/api'
import EmptyState from '@/components/EmptyState.vue'
import ActivityList from '@/components/flow/ActivityList.vue'
import ListSkeleton from '@/components/ListSkeleton.vue'
import PageHeader from '@/components/PageHeader.vue'
import { Button } from '@/components/ui/button'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import type { ActivityItem, ActivityPage, SourceSummary } from '@/types'

import AuditList from './settings/AuditList.vue'

// 记录：时间线把一次回传、审核、推送、通知按时间串起来，可按来源、只看问题筛选；审计日志是另一个标签页。
// 筛选条件放在地址里（?tab=audit、?source=<来源标识>、?result=problem），刷新时保留，详情页的“全部记录”也靠它带过来。
// 来源在地址里用标识，和推送页的 ?source= 一致；接口按来源 ID 筛选，查询前换算。纯数字的值是早先按 ID 写的链接
// （来源标识必须以字母开头，不会是纯数字），直接当 ID 用。
const route = useRoute()
const router = useRouter()

const tab = computed({
  get: () => (route.query.tab === 'audit' ? 'audit' : 'timeline'),
  set: (v) => void router.replace({ query: { tab: v === 'audit' ? 'audit' : undefined } }),
})
const source = computed({
  get: () => (typeof route.query.source === 'string' ? route.query.source : ''),
  set: (v) => void router.replace({ query: { ...route.query, source: v || undefined } }),
})
const problems = computed({
  get: () => route.query.result === 'problem',
  set: (v) => void router.replace({ query: { ...route.query, result: v ? 'problem' : undefined } }),
})

const sources = ref<SourceSummary[]>([])
const sourcesLoaded = api
  .get<SourceSummary[]>('/sources')
  .then((r) => (sources.value = r.data))
  .catch(() => {
    // 拿不到来源列表时只是没有筛选项
  })
async function sourceID(v: string) {
  if (/^\d+$/.test(v)) return v
  await sourcesLoaded
  const id = sources.value.find((s) => s.key === v)?.id
  return id ? String(id) : ''
}
const items = ref<ActivityItem[] | null>(null)
const next = ref<string | null>(null)
const loading = ref(false)

async function load(append = false) {
  loading.value = true
  try {
    const params: Record<string, string> = {}
    if (source.value) {
      const id = await sourceID(source.value)
      // 地址里的来源不存在（已删除或写错）时不筛选
      if (id) params.source = id
    }
    if (problems.value) params.result = 'problem'
    if (append && next.value) params.before = next.value
    const { data } = await api.get<ActivityPage>('/activity', { params })
    items.value = append && items.value ? [...items.value, ...data.items] : data.items
    next.value = data.next
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    loading.value = false
  }
}

watch([source, problems], () => {
  items.value = null
  void load()
})

onMounted(() => void load())
</script>

<template>
  <PageHeader />

  <Tabs v-model="tab">
    <TabsList class="mb-4">
      <TabsTrigger value="timeline"><ActivityIcon /> 时间线</TabsTrigger>
      <TabsTrigger value="audit"><ScrollTextIcon /> 审计日志</TabsTrigger>
    </TabsList>

    <TabsContent value="timeline" class="grid gap-4">
      <div class="flex flex-wrap items-center gap-x-4 gap-y-2">
        <NativeSelect v-model="source" class="w-48" aria-label="按来源筛选">
          <NativeSelectOption value="">全部来源</NativeSelectOption>
          <NativeSelectOption v-for="s in sources" :key="s.id" :value="s.key">{{
            s.name
          }}</NativeSelectOption>
        </NativeSelect>
        <label class="flex items-center gap-2 text-sm">
          <Switch v-model="problems" /> 仅显示失败与警告
        </label>
        <span class="text-muted-foreground text-xs"
          >按来源筛选时不包含通知（通知不归属于特定来源）</span
        >
      </div>

      <ListSkeleton v-if="!items" :rows="5" />
      <EmptyState
        v-else-if="!items.length"
        :icon="ActivityIcon"
        :title="problems ? '暂无失败或警告' : '暂无记录'"
        description="此处记录回传、审核、推送与通知事件。"
      />
      <div v-else class="bg-card rounded-lg border p-4">
        <ActivityList :items="items" />
        <div v-if="next" class="mt-4 flex justify-center">
          <Button variant="outline" size="sm" :disabled="loading" @click="load(true)"
            >加载更多</Button
          >
        </div>
      </div>
    </TabsContent>

    <TabsContent value="audit">
      <AuditList />
    </TabsContent>
  </Tabs>
</template>
