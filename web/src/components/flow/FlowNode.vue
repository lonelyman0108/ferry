<script setup lang="ts">
import EntityIcon from '@/components/EntityIcon.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import type { Mark } from '@/lib/icons'
import type { Tone } from '@/lib/tone'

// 流向图上的一张节点卡：标识（品牌图标、来源头像或图标块）、名称、副标题、状态徽章，下面一排概要数字。
// 左右两侧的小圆点是连线的端口；line 是窄屏上替代连线的一句去向说明，宽屏上画了连线，只留给读屏。
// 宽窄按所在流向图的容器宽度判断，和流向图的三列切换用同一个断点。
// selected 表示当前页面就是这个对象，此时不是按钮，点了也不跳转。
defineProps<{
  mark: Mark
  title: string
  subtitle?: string
  mono?: boolean
  badges?: { label: string; tone: Tone }[]
  meta?: { label: string; value: string; tone?: 'warning' | 'danger' }[]
  line?: string
  muted?: boolean
  selected?: boolean
  portLeft?: boolean
  portRight?: boolean
}>()
defineEmits<{ select: [] }>()
</script>

<template>
  <component
    :is="selected ? 'div' : 'button'"
    v-bind="selected ? { 'aria-current': 'page' } : { type: 'button' }"
    class="bg-card relative block w-full rounded-lg border px-3 py-2.5 text-left"
    :class="[
      muted && 'border-dashed bg-transparent',
      selected
        ? 'border-primary ring-primary/20 ring-[3px]'
        : 'hover:border-ring/60 focus-visible:ring-ring/50 transition-[border-color,box-shadow] outline-none focus-visible:ring-[3px]',
    ]"
    @click="!selected && $emit('select')"
  >
    <span
      v-if="portLeft"
      class="bg-background absolute top-1/2 -left-[5px] hidden size-[9px] -translate-y-1/2 rounded-full border-[1.5px] border-(--edge) @xl:block"
      :class="selected && 'border-primary'"
    />
    <span
      v-if="portRight"
      class="bg-background absolute top-1/2 -right-[5px] hidden size-[9px] -translate-y-1/2 rounded-full border-[1.5px] border-(--edge) @xl:block"
      :class="selected && 'border-primary'"
    />
    <span class="flex min-w-0 items-center gap-2.5">
      <EntityIcon :mark="mark" size="sm" />
      <span class="min-w-0 flex-1">
        <span class="block truncate text-sm leading-5 font-semibold">{{ title }}</span>
        <span
          v-if="subtitle"
          class="text-muted-foreground block truncate text-xs leading-4"
          :class="mono && 'font-mono'"
          >{{ subtitle }}</span
        >
      </span>
    </span>
    <span v-if="badges?.length" class="mt-2 flex flex-wrap gap-1.5">
      <StatusBadge v-for="b in badges" :key="b.label" :tone="b.tone" dot>{{ b.label }}</StatusBadge>
    </span>
    <span v-if="meta?.length" class="mt-2.5 grid grid-cols-3 gap-1 border-t pt-2">
      <span v-for="m in meta" :key="m.label" class="min-w-0">
        <span class="text-muted-foreground block text-xs leading-4">{{ m.label }}</span>
        <span
          class="block truncate text-sm leading-5 font-medium"
          :class="
            m.tone === 'danger' ? 'text-destructive' : m.tone === 'warning' ? 'text-warning' : ''
          "
          >{{ m.value }}</span
        >
      </span>
    </span>
    <span v-if="line" class="text-muted-foreground mt-2 block text-sm @xl:sr-only">{{ line }}</span>
  </component>
</template>
