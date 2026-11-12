<script setup lang="ts">
import { ArrowRightIcon, CheckIcon, RefreshCwIcon, XIcon } from '@lucide/vue'
import { computed, ref } from 'vue'

import StatusBadge from '@/components/StatusBadge.vue'
import { Button } from '@/components/ui/button'
import { fmtAgo, fmtUntil } from '@/format'
import { icons } from '@/lib/icons'
import type { AttentionItem } from '@/types'

import { clientName, itemLabels, useInboxActions } from './inbox'

// 收件箱里的一条，带就地操作。处理成功后发 done，由外面刷新数据；「知道了」发 dismiss。
const props = defineProps<{ item: AttentionItem }>()
const emit = defineEmits<{ done: []; dismiss: [] }>()

const actions = useInboxActions()
const busy = ref(false)
const meta = computed(() => itemLabels[props.item.kind])

async function run(fn: () => Promise<boolean>) {
  busy.value = true
  try {
    if (await fn()) emit('done')
  } finally {
    busy.value = false
  }
}

const who = computed(() => {
  const it = props.item
  if (it.subscription_id) return `${it.subscription_name} · ${clientName(it.client)}`
  return it.source_name ?? ''
})
const identity = computed(() =>
  props.item.identity_key && props.item.identity_key !== 'default' ? props.item.identity_key : '',
)
</script>

<template>
  <div class="grid gap-1.5">
    <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
      <StatusBadge :tone="meta.tone">{{ meta.label }}</StatusBadge>
      <span class="font-medium">{{ who }}</span>
      <template v-if="item.destination_name">
        <ArrowRightIcon class="text-muted-foreground size-3.5" aria-label="推送至" />
        <span class="font-medium">{{ item.destination_name }}</span>
      </template>
    </div>

    <p class="text-muted-foreground text-sm">
      <template v-if="item.kind === 'pending_review'">
        <span class="text-foreground font-mono">{{ item.field }}</span>
        <template v-if="identity"> · {{ identity }}</template>
        · 新值 <span class="text-foreground font-mono break-all">{{ item.value }}</span>
      </template>
      <template v-else-if="item.kind === 'push_failed' || item.kind === 'push_retrying'">
        {{ item.error || '推送失败' }}
        <template v-if="item.attempts"> · 已尝试 {{ item.attempts }} 次</template>
      </template>
      <template v-else-if="item.kind === 'expiring' || item.kind === 'expired'">
        <span class="text-foreground font-mono">{{ item.field }}</span>
        <template v-if="identity"> · {{ identity }}</template>
        · {{ item.kind === 'expired' ? '已过期' : fmtUntil(item.at) + '过期' }}
      </template>
      <template v-else-if="item.kind === 'outdated'">MITM 域名已变更，需更新订阅</template>
      <template v-else>创建于 {{ fmtAgo(item.at) }}，从未连接</template>
    </p>

    <p
      v-if="item.kind === 'expiring' || item.kind === 'expired'"
      class="text-muted-foreground flex items-start gap-1.5 text-sm"
    >
      <component :is="icons.subscription" class="mt-0.5 size-3.5 shrink-0" />
      在设备上打开{{ item.source_name }}的 App 后，新的登录态将自动回传
    </p>

    <div class="mt-1 flex flex-wrap gap-2">
      <template v-if="item.kind === 'pending_review'">
        <Button size="sm" :disabled="busy" @click="run(() => actions.review(item, true))"
          ><CheckIcon /> 通过</Button
        >
        <Button
          size="sm"
          variant="outline"
          :disabled="busy"
          @click="run(() => actions.review(item, false))"
          ><XIcon /> 驳回</Button
        >
      </template>
      <template v-else-if="item.kind === 'push_failed' || item.kind === 'push_retrying'">
        <Button size="sm" :disabled="busy" @click="run(() => actions.retry(item))"
          ><RefreshCwIcon :class="busy && 'animate-spin'" /> 立即重试</Button
        >
        <Button size="sm" variant="outline" @click="actions.openDestination()">查看目的地</Button>
      </template>
      <template v-else-if="item.kind === 'outdated'">
        <Button size="sm" variant="outline" @click="actions.openInstall(item)"
          ><RefreshCwIcon /> 重新安装</Button
        >
      </template>
      <template v-else-if="item.kind === 'never_connected'">
        <Button size="sm" variant="outline" @click="actions.openInstall(item)">安装引导</Button>
        <Button size="sm" variant="ghost" @click="emit('dismiss')">忽略</Button>
      </template>
      <Button v-else size="sm" variant="outline" @click="emit('dismiss')">忽略</Button>
    </div>
  </div>
</template>
