<script setup lang="ts">
import { computed } from 'vue'

import { useOverview } from '@/stores/overview'

// 顶栏的一句话状态，点了回总览并展开待处理。
const overview = useOverview()
const s = computed(() => overview.summary)
</script>

<template>
  <router-link
    v-if="s"
    :to="{ path: '/', query: { inbox: 'open' } }"
    class="bg-card hover:bg-accent inline-flex h-8 min-w-0 items-center gap-2 rounded-md border px-3 text-sm transition-colors"
  >
    <span
      class="size-2 shrink-0 rounded-full"
      :class="s.action ? 'bg-warning' : 'bg-success'"
      aria-hidden="true"
    />
    <span class="truncate font-medium">{{ s.action ? `${s.action} 件事要处理` : '全部通着' }}</span>
    <span v-if="s.notice" class="text-muted-foreground hidden truncate lg:inline"
      >另有 {{ s.notice }} 条提醒</span
    >
  </router-link>
</template>
