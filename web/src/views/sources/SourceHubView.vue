<script setup lang="ts">
import {
  AlertTriangleIcon,
  DownloadIcon,
  ExternalLinkIcon,
  GitBranchIcon,
  GitCompareArrowsIcon,
  GlobeIcon,
  PackageSearchIcon,
  PlusIcon,
  RefreshCwIcon,
  SearchIcon,
  ShieldAlertIcon,
} from '@lucide/vue'
import { computed, onMounted, reactive, ref } from 'vue'
import { toast } from 'vue-sonner'

import { api, errorText } from '@/api'
import EmptyState from '@/components/EmptyState.vue'
import FormDialog from '@/components/FormDialog.vue'
import FormField from '@/components/FormField.vue'
import HintText from '@/components/HintText.vue'
import ListSkeleton from '@/components/ListSkeleton.vue'
import PageHeader from '@/components/PageHeader.vue'
import RowActions from '@/components/RowActions.vue'
import SourceAvatar from '@/components/SourceAvatar.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { fmtAgo } from '@/format'
import { useFormCheck } from '@/lib/form'
import type { Repo, RepoItem, RepoItemStatus } from '@/types'

import SourceDiffDialog from './SourceDiffDialog.vue'
import SourceImportDialog, { type ImportBatchEntry } from './SourceImportDialog.vue'

const repos = ref<Repo[] | null>(null)
const refreshingAll = ref(false)
const refreshingRepoId = ref<number | null>(null)
const togglingRepoId = ref<number | null>(null)
const query = ref('')

// 多选集合，键为 `${repo.id}:${item.key}:${item.path}`。
const selectedKeys = ref<Record<string, boolean>>({})

const showImport = ref(false)
const importBatch = ref<ImportBatchEntry[]>([])

const diffTarget = ref<{ repo: Repo; item: RepoItem } | null>(null)

const repoForm = reactive({
  open: false,
  id: 0,
  official: false,
  name: '',
  url: '',
  enabled: true,
  busy: false,
})

const repoCheck = useFormCheck(() => ({
  name: !repoForm.name.trim() && '请填写仓库名称',
  url: !repoForm.url.trim() && '请填写仓库地址',
}))

async function load(refresh = false) {
  if (refresh) refreshingAll.value = true
  try {
    repos.value = (await api.get<Repo[]>('/repos', { params: refresh ? { refresh: 1 } : {} })).data
    selectedKeys.value = {}
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    refreshingAll.value = false
  }
}

onMounted(() => {
  void load()
})

function itemID(repoId: number, it: RepoItem): string {
  return `${repoId}:${it.key}:${it.path}`
}

function isSelected(repoId: number, it: RepoItem): boolean {
  return !!selectedKeys.value[itemID(repoId, it)]
}

function toggleSelect(repoId: number, it: RepoItem, checked: boolean | 'indeterminate') {
  const id = itemID(repoId, it)
  if (checked === true) {
    selectedKeys.value = { ...selectedKeys.value, [id]: true }
  } else {
    const next = { ...selectedKeys.value }
    delete next[id]
    selectedKeys.value = next
  }
}

const selectedEntries = computed(() => {
  const list: { repo: Repo; item: RepoItem }[] = []
  for (const r of repos.value ?? []) {
    if (!r.enabled) continue
    for (const it of r.items) {
      if (it.source && selectedKeys.value[itemID(r.id, it)]) {
        list.push({ repo: r, item: it })
      }
    }
  }
  return list
})

function openBatchImport(entries: { repo: Repo; item: RepoItem }[]) {
  importBatch.value = entries
    .filter((e) => !!e.item.source)
    .map((e) => ({
      meta: {
        version: e.item.version,
        author: e.item.author,
        homepage: e.item.homepage,
        tags: e.item.tags,
      },
      source: e.item.source!,
      origin: {
        repo_id: e.repo.id,
        repo_url: e.repo.url,
        path: e.item.path,
        version: e.item.version,
      },
    }))
  if (importBatch.value.length) {
    showImport.value = true
  }
}

function openSingleImport(repo: Repo, item: RepoItem) {
  openBatchImport([{ repo, item }])
}

function openDiff(repo: Repo, item: RepoItem) {
  diffTarget.value = { repo, item }
}

function filteredItems(repo: Repo): RepoItem[] {
  const q = query.value.trim().toLowerCase()
  if (!q) return repo.items
  return repo.items.filter(
    (it) =>
      it.name.toLowerCase().includes(q) ||
      it.key.toLowerCase().includes(q) ||
      (it.description ?? '').toLowerCase().includes(q) ||
      (it.author ?? '').toLowerCase().includes(q) ||
      (it.tags ?? []).some((t) => t.toLowerCase().includes(q)),
  )
}

const statusMeta: Record<
  RepoItemStatus,
  { label: string; tone: 'neutral' | 'success' | 'warning' }
> = {
  not_imported: { label: '未导入', tone: 'neutral' },
  imported: { label: '已导入', tone: 'success' },
  update_available: { label: '有更新', tone: 'warning' },
  local_modified: { label: '本地已修改', tone: 'warning' },
}

function openAddRepo() {
  repoCheck.reset()
  Object.assign(repoForm, {
    open: true,
    id: 0,
    official: false,
    name: '',
    url: '',
    enabled: true,
    busy: false,
  })
}

function openEditRepo(repo: Repo) {
  repoCheck.reset()
  Object.assign(repoForm, {
    open: true,
    id: repo.id,
    official: repo.official,
    name: repo.name,
    url: repo.url,
    enabled: repo.enabled,
    busy: false,
  })
}

async function saveRepo() {
  if (!repoCheck.ok()) return
  repoForm.busy = true
  try {
    const body = {
      name: repoForm.name.trim(),
      url: repoForm.url.trim(),
      enabled: repoForm.enabled,
    }
    if (repoForm.id) {
      await api.put(`/repos/${repoForm.id}`, body)
    } else {
      await api.post('/repos', body)
    }
    toast.success('已保存')
    repoForm.open = false
    await load()
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    repoForm.busy = false
  }
}

async function toggleRepo(repo: Repo, enabled: boolean) {
  togglingRepoId.value = repo.id
  try {
    await api.put(`/repos/${repo.id}`, { enabled })
    await load()
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    togglingRepoId.value = null
  }
}

async function refreshRepo(repo: Repo) {
  refreshingRepoId.value = repo.id
  try {
    const { data } = await api.post<Repo>(`/repos/${repo.id}/refresh`, {})
    if (repos.value) {
      repos.value = repos.value.map((r) => (r.id === repo.id ? data : r))
    }
    if (data.last_error) {
      toast.error(data.last_error)
    } else {
      toast.success(`已刷新「${repo.name}」`)
    }
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    refreshingRepoId.value = null
  }
}

async function removeRepo(repo: Repo) {
  try {
    await api.delete(`/repos/${repo.id}`)
    toast.success('已删除')
    await load()
  } catch (e) {
    toast.error(errorText(e))
  }
}
</script>

<template>
  <PageHeader>
    <template #actions>
      <Button variant="outline" :disabled="refreshingAll || !repos?.length" @click="load(true)">
        <RefreshCwIcon :class="refreshingAll && 'animate-spin'" /> 刷新全部
      </Button>
      <Button @click="openAddRepo"><PlusIcon /> 添加仓库</Button>
    </template>
  </PageHeader>

  <div class="grid gap-5">
    <div class="flex flex-wrap items-center gap-3">
      <div class="relative min-w-56 flex-1 sm:max-w-xs">
        <SearchIcon
          class="text-muted-foreground pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2"
        />
        <Input v-model="query" placeholder="筛选来源名称、标识或标签" class="pl-9" />
      </div>
      <div v-if="selectedEntries.length" class="flex flex-wrap items-center gap-2 sm:ml-auto">
        <span class="text-muted-foreground text-xs">已选择 {{ selectedEntries.length }} 项</span>
        <Button size="sm" @click="openBatchImport(selectedEntries)">
          <DownloadIcon /> 导入所选
        </Button>
      </div>
    </div>

    <ListSkeleton v-if="!repos" :rows="3" />
    <EmptyState
      v-else-if="!repos.length"
      :icon="PackageSearchIcon"
      title="暂无来源仓库"
      description="添加官方或第三方来源仓库后，即可浏览并批量导入来源规则。"
    >
      <Button @click="openAddRepo"><PlusIcon /> 添加仓库</Button>
    </EmptyState>

    <template v-else>
      <section v-for="repo in repos" :key="repo.id" class="grid gap-3 rounded-xl border p-4">
        <div class="flex flex-wrap items-start gap-3">
          <div class="min-w-0 flex-1">
            <div class="flex flex-wrap items-center gap-2">
              <GitBranchIcon class="text-muted-foreground size-4 shrink-0" />
              <h2 class="font-semibold">{{ repo.name }}</h2>
              <StatusBadge v-if="repo.official" tone="info">官方仓库</StatusBadge>
              <StatusBadge v-if="!repo.github_url" tone="neutral">非 GitHub</StatusBadge>
              <StatusBadge v-if="!repo.enabled" dot>已停用</StatusBadge>
            </div>
            <div class="text-muted-foreground mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs">
              <span class="font-mono break-all">{{ repo.url }}</span>
              <span v-if="repo.last_fetched_at"> 最近同步 {{ fmtAgo(repo.last_fetched_at) }} </span>
            </div>
            <div
              v-if="!repo.github_url"
              class="text-muted-foreground mt-1.5 flex items-center gap-1.5 text-xs"
            >
              <GlobeIcon class="size-3.5 shrink-0" />
              <span>该仓库非 GitHub 地址，直接连接拉取，不适用 GitHub 网络设置。</span>
            </div>
            <p v-if="repo.last_error" class="text-destructive mt-1.5 text-xs break-words">
              {{ repo.last_error }}
            </p>
          </div>

          <RowActions
            :name="repo.name"
            :enabled="repo.enabled"
            :toggling="togglingRepoId === repo.id"
            :editable="!repo.official"
            delete-title="删除来源仓库？"
            delete-description="删除仓库不会影响已导入的来源。"
            :delete-blocked="repo.official ? '官方仓库不可删除，可停用' : ''"
            @edit="openEditRepo(repo)"
            @remove="removeRepo(repo)"
            @toggle="(v) => toggleRepo(repo, v)"
          >
            <Button
              variant="ghost"
              size="icon-sm"
              :disabled="refreshingRepoId === repo.id"
              :aria-label="`刷新「${repo.name}」`"
              title="刷新仓库"
              @click="refreshRepo(repo)"
            >
              <RefreshCwIcon :class="refreshingRepoId === repo.id && 'animate-spin'" />
            </Button>
          </RowActions>
        </div>

        <p v-if="!repo.enabled" class="text-muted-foreground text-xs">
          该仓库已停用，启用后显示来源列表。
        </p>
        <p v-else-if="!filteredItems(repo).length" class="text-muted-foreground text-xs">
          {{ repo.items.length ? '无匹配的来源条目。' : '该仓库暂无收录来源。' }}
        </p>

        <div v-else class="grid gap-3 sm:grid-cols-2">
          <article
            v-for="item in filteredItems(repo)"
            :key="item.key + ':' + item.path"
            class="flex flex-col gap-3 rounded-lg border p-3.5 transition"
            :class="
              item.violations?.length || item.fetch_error
                ? 'border-destructive/50 bg-destructive/[0.02]'
                : 'bg-card'
            "
          >
            <div class="flex items-start gap-3">
              <div class="pt-0.5">
                <Checkbox
                  :disabled="!item.source"
                  :model-value="isSelected(repo.id, item)"
                  :aria-label="`选择「${item.name}」`"
                  @update:model-value="(v) => toggleSelect(repo.id, item, v)"
                />
              </div>
              <SourceAvatar :name="item.name" :skey="item.key" :icon="item.icon ?? ''" size="sm" />
              <div class="min-w-0 flex-1">
                <div class="flex flex-wrap items-center gap-1.5">
                  <span class="font-medium break-words">{{ item.name }}</span>
                  <span class="text-muted-foreground font-mono text-xs">v{{ item.version }}</span>
                  <StatusBadge :tone="statusMeta[item.status].tone" dot>
                    {{ statusMeta[item.status].label }}
                  </StatusBadge>
                  <StatusBadge v-if="item.violations?.length" tone="danger">
                    违反安全规则
                  </StatusBadge>
                </div>
                <div class="text-muted-foreground mt-0.5 flex flex-wrap gap-x-2 text-xs">
                  <span class="font-mono">{{ item.key }}</span>
                  <span v-if="item.author">作者：{{ item.author }}</span>
                </div>
              </div>
            </div>

            <p v-if="item.description" class="text-muted-foreground text-xs leading-relaxed">
              {{ item.description }}
            </p>

            <div v-if="item.tags?.length" class="flex flex-wrap gap-1.5">
              <StatusBadge v-for="t in item.tags" :key="t" tone="neutral">{{ t }}</StatusBadge>
            </div>

            <div
              v-if="item.fetch_error"
              class="text-destructive bg-destructive/5 border-destructive/30 rounded border px-2.5 py-2 text-xs"
            >
              {{ item.fetch_error }}
            </div>

            <div
              v-if="item.violations?.length"
              class="text-destructive bg-destructive/5 border-destructive/30 grid gap-1 rounded border px-2.5 py-2 text-xs"
            >
              <div class="flex items-center gap-1 font-medium">
                <ShieldAlertIcon class="size-3.5 shrink-0" />
                安全规则校验未通过
              </div>
              <ul class="list-disc space-y-0.5 pl-4">
                <li v-for="(v, vi) in item.violations" :key="vi">{{ v.message }}</li>
              </ul>
            </div>

            <div
              v-if="item.warnings?.length"
              class="text-warning bg-warning/5 border-warning/30 grid gap-0.5 rounded border px-2.5 py-2 text-xs"
            >
              <div v-for="(w, wi) in item.warnings" :key="wi">注意：{{ w.message }}</div>
            </div>

            <div class="mt-auto flex flex-wrap items-center gap-2 border-t pt-2.5">
              <a
                v-if="item.readme_url"
                :href="item.readme_url"
                target="_blank"
                rel="noopener"
                class="text-muted-foreground hover:text-foreground inline-flex items-center gap-1 text-xs underline-offset-2 hover:underline"
              >
                <ExternalLinkIcon class="size-3.5" /> 查看说明
              </a>
              <div class="ml-auto flex items-center gap-2">
                <Button
                  v-if="item.status === 'update_available' && item.source && item.local_source"
                  size="sm"
                  @click="openDiff(repo, item)"
                >
                  <GitCompareArrowsIcon /> 查看更新
                </Button>
                <Button
                  v-else-if="item.source"
                  :variant="item.status === 'not_imported' ? 'default' : 'outline'"
                  size="sm"
                  @click="openSingleImport(repo, item)"
                >
                  <DownloadIcon /> {{ item.status === 'not_imported' ? '导入' : '重新导入' }}
                </Button>
              </div>
            </div>
          </article>
        </div>
      </section>
    </template>
  </div>

  <FormDialog
    v-model:open="repoForm.open"
    :title="repoForm.id ? '编辑来源仓库' : '添加来源仓库'"
    :description="
      repoForm.id
        ? repoForm.name
        : '支持 GitHub 仓库地址（自动换算为 index.json）或任意 HTTP(S) index.json 地址'
    "
    :icon="GitBranchIcon"
    :busy="repoForm.busy"
    @submit="saveRepo"
  >
    <Alert class="border-warning/40 bg-warning/5">
      <AlertTriangleIcon class="text-warning size-4" />
      <AlertDescription class="text-xs leading-relaxed">
        第三方仓库由外部维护者提供，其中的来源规则与有效性探测可能包含非预期域名。添加或导入前请核对匹配规则与探测地址。
      </AlertDescription>
    </Alert>

    <FormField label="仓库名称" for="repo-name" :error="repoCheck.errors.name">
      <Input id="repo-name" v-model="repoForm.name" placeholder="如 社区来源仓库" />
    </FormField>

    <FormField
      label="仓库地址"
      for="repo-url"
      hint="如 https://github.com/owner/repo 或 https://example.com/hub/index.json"
      :error="repoCheck.errors.url"
    >
      <Input
        id="repo-url"
        v-model="repoForm.url"
        class="font-mono"
        placeholder="https://github.com/owner/repo"
      />
    </FormField>

    <HintText
      v-if="repoForm.url.trim() && !/github\.com|githubusercontent\.com/i.test(repoForm.url)"
    >
      该地址非 GitHub 域名，拉取时将直接连接，不适用 GitHub 网络设置。
    </HintText>

    <div class="flex items-center justify-between rounded-lg border px-3 py-2.5">
      <span class="text-sm font-medium">启用仓库</span>
      <Switch v-model="repoForm.enabled" aria-label="启用仓库" />
    </div>
  </FormDialog>

  <SourceImportDialog
    :open="showImport"
    :initial-batch="importBatch"
    @close="showImport = false"
    @imported="load()"
  />

  <SourceDiffDialog
    :open="!!diffTarget"
    :repo="diffTarget?.repo ?? null"
    :item="diffTarget?.item ?? null"
    @close="diffTarget = null"
    @updated="load()"
  />
</template>
