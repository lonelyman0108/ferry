<script setup lang="ts">
import type { Component, HTMLAttributes } from 'vue'

import { cn } from '@/lib/utils'
import { type Tone, toneTile } from '@/lib/tone'

// 图标块：底色加图标。表示对象或概念时用默认的中性色，颜色只留给状态（成功、警告、失败等）。
// xs 用在行内的小列表里，sm、md、lg 与 BrandIcon、SourceAvatar 同档。
const props = withDefaults(
  defineProps<{
    icon: Component
    tone?: Tone
    size?: 'xs' | 'sm' | 'md' | 'lg'
    class?: HTMLAttributes['class']
  }>(),
  { tone: 'neutral', size: 'md', class: undefined },
)

const box = {
  xs: 'size-5 rounded',
  sm: 'size-7 rounded-md',
  md: 'size-9 rounded-lg',
  lg: 'size-11 rounded-xl',
}
// 尺寸写在图标自己身上：放进命令面板、按钮这类会统一设图标尺寸的容器里也不被改掉。
const glyph = { xs: 'size-3', sm: 'size-3.5', md: 'size-4.5', lg: 'size-5.5' }
</script>

<template>
  <span
    :class="
      cn('inline-flex shrink-0 items-center justify-center', box[size], toneTile[tone], props.class)
    "
  >
    <component :is="icon" :class="glyph[size]" />
  </span>
</template>
