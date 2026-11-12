<script setup lang="ts">
import { InfoIcon, TriangleAlertIcon } from '@lucide/vue'

// 一句提示：info 是说明（蓝色信息图标），warning 是有风险、需要留意的情况（琥珀色警示图标，文字也用琥珀色），
// 如访问地址为 http 时的明文传输，和列表上“明文传输”徽章同一种颜色。
withDefaults(defineProps<{ tone?: 'info' | 'warning' }>(), { tone: 'info' })
</script>

<template>
  <p
    class="flex gap-2 text-sm"
    :class="tone === 'warning' ? 'text-warning' : 'text-muted-foreground'"
  >
    <TriangleAlertIcon v-if="tone === 'warning'" class="mt-0.5 size-4 shrink-0" />
    <InfoIcon v-else class="text-info mt-0.5 size-4 shrink-0" />
    <span><slot /></span>
  </p>
</template>
