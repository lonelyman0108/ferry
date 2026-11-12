<script setup lang="ts">
import { GlobeIcon, PlusIcon } from '@lucide/vue'
import { onMounted, reactive, ref } from 'vue'
import { toast } from 'vue-sonner'

import { api, errorText } from '@/api'
import EmptyState from '@/components/EmptyState.vue'
import FormField from '@/components/FormField.vue'
import FormDialog from '@/components/FormDialog.vue'
import HintText from '@/components/HintText.vue'
import IconTile from '@/components/IconTile.vue'
import ListRow from '@/components/ListRow.vue'
import ListSkeleton from '@/components/ListSkeleton.vue'
import PageHeader from '@/components/PageHeader.vue'
import RowActions from '@/components/RowActions.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useFormCheck } from '@/lib/form'
import type { Endpoint, Subscription } from '@/types'

const busy = ref(false)

// ---- 访问地址 ----
const endpoints = ref<Endpoint[] | null>(null)
const subs = ref<Subscription[]>([])
const epForm = reactive({ open: false, id: 0, name: '', base_url: '', note: '' })

async function loadEndpoints() {
  const [e, s] = await Promise.all([
    api.get<Endpoint[]>('/endpoints'),
    api.get<Subscription[]>('/subscriptions'),
  ])
  endpoints.value = e.data
  subs.value = s.data
}

// 用到这个地址的订阅数（主地址或备用地址）；还有订阅在用时不能删除。
const usage = (e: Endpoint) =>
  subs.value.filter((s) => s.endpoint_id === e.id || s.fallback_id === e.id).length

const epCheck = useFormCheck(() => ({
  name: !epForm.name.trim() && '请填写名称',
  base_url: !epForm.base_url.trim() && '请填写地址',
}))

function editEndpoint(e?: Endpoint) {
  epCheck.reset()
  Object.assign(
    epForm,
    e
      ? { open: true, ...e }
      : { open: true, id: 0, name: '', base_url: window.location.origin, note: '' },
  )
}

async function saveEndpoint() {
  if (!epCheck.ok()) return
  busy.value = true
  try {
    const body = { name: epForm.name, base_url: epForm.base_url, note: epForm.note }
    if (epForm.id) await api.put(`/endpoints/${epForm.id}`, body)
    else await api.post('/endpoints', body)
    toast.success('已保存')
    epForm.open = false
    await loadEndpoints()
  } catch (e) {
    toast.error(errorText(e))
  } finally {
    busy.value = false
  }
}

async function removeEndpoint(e: Endpoint) {
  try {
    await api.delete(`/endpoints/${e.id}`)
    toast.success('已删除')
    await loadEndpoints()
  } catch (err) {
    toast.error(errorText(err))
  }
}

onMounted(async () => {
  try {
    await loadEndpoints()
  } catch (e) {
    toast.error(errorText(e))
  }
})
</script>

<template>
  <PageHeader>
    <template #actions>
      <!-- 窄屏时操作区单独占一行，保留文字，不只显示图标 -->
      <Button @click="editEndpoint()"><PlusIcon /> 新建访问地址</Button>
    </template>
  </PageHeader>

  <div class="grid gap-3">
    <HintText>可配置多个访问地址，如局域网地址与公网地址。</HintText>
    <ListSkeleton v-if="!endpoints" :rows="2" />
    <EmptyState
      v-else-if="!endpoints.length"
      :icon="GlobeIcon"
      title="暂无访问地址"
      description="设备通过访问地址拉取读取规则、回传登录态，新建订阅前需至少添加一个访问地址。"
    >
      <Button @click="editEndpoint()"><PlusIcon /> 新建访问地址</Button>
    </EmptyState>
    <template v-else>
      <ListRow v-for="e in endpoints" :key="e.id">
        <template #icon><IconTile :icon="GlobeIcon" /></template>
        <template #title>{{ e.name }}</template>
        <template #badges>
          <StatusBadge tone="neutral">{{
            usage(e) ? `${usage(e)} 个订阅使用中` : '未被订阅使用'
          }}</StatusBadge>
        </template>
        <template #description>
          <span class="font-mono break-all">{{ e.base_url }}</span>
          <span v-if="e.note">{{ e.note }}</span>
        </template>
        <template #actions>
          <RowActions
            :name="e.name"
            delete-title="删除访问地址？"
            :delete-blocked="usage(e) ? '被订阅使用的访问地址无法删除' : ''"
            @edit="editEndpoint(e)"
            @remove="removeEndpoint(e)"
          />
        </template>
      </ListRow>
    </template>
  </div>

  <FormDialog
    v-model:open="epForm.open"
    :title="epForm.id ? '编辑访问地址' : '新建访问地址'"
    :description="epForm.id ? epForm.name : '设备访问 Ferry 所用的地址'"
    :icon="GlobeIcon"
    :busy="busy"
    @submit="saveEndpoint"
  >
    <FormField label="名称" for="ep-name" :error="epCheck.errors.name">
      <Input id="ep-name" v-model="epForm.name" placeholder="如 LAN、Public" />
    </FormField>
    <FormField
      label="地址"
      for="ep-url"
      hint="须可从设备访问；经反向代理部署在子路径下时，须包含该路径"
      :error="epCheck.errors.base_url"
    >
      <Input
        id="ep-url"
        v-model="epForm.base_url"
        class="font-mono"
        placeholder="http://host:8080"
      />
    </FormField>
    <FormField label="备注" for="ep-note" optional>
      <Input id="ep-note" v-model="epForm.note" />
    </FormField>
  </FormDialog>
</template>
