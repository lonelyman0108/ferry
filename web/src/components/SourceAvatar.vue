<script setup lang="ts">
import { computed, type HTMLAttributes } from 'vue'

import { cn } from '@/lib/utils'
import { useSourceIcons } from '@/stores/sourceIcons'

// 来源头像：有自定义图标时显示图标，否则显示名称首字，底色按 key 从固定色板里取，同一来源在哪儿都是同一个颜色。
// 尺寸与 IconTile、BrandIcon 对齐，可以在同一列表里混用。
// 不传 icon 时从 useSourceIcons 查；传了（包括空字符串）就用传入的，编辑器里预览未保存的图标靠这个。
// disabled 表示来源已停用：头像变灰、半透明，各处展示来源头像时都传入，和来源列表一致。
const props = withDefaults(
  defineProps<{
    name: string
    // 来源 key，决定底色并用来查自定义图标
    skey: string
    icon?: string
    size?: 'xs' | 'sm' | 'md' | 'lg'
    disabled?: boolean
    class?: HTMLAttributes['class']
  }>(),
  { icon: undefined, size: 'md', disabled: false, class: undefined },
)

const icons = useSourceIcons()
const src = computed(() => {
  if (props.icon !== undefined) return props.icon
  icons.ensure()
  return icons.byKey[props.skey] ?? ''
})

// 浅色用 800 号字、深色用 200 号字，两种模式下与各自底色的对比度都在 4.5:1 以上。
// 避开主色（紫）和成功、警告、失败、提示几种状态色的色相，各色之间在深色下也能分清。
// 橄榄（色相 110）用自定义的 oklch 值：Tailwind 自带的 lime 接近成功色的绿。
const palette = [
  'bg-blue-500/15 text-blue-800 dark:bg-blue-400/20 dark:text-blue-200',
  'bg-cyan-500/15 text-cyan-800 dark:bg-cyan-400/20 dark:text-cyan-200',
  'bg-[oklch(0.75_0.13_110/0.15)] text-[oklch(0.44_0.09_110)] dark:bg-[oklch(0.82_0.13_110/0.2)] dark:text-[oklch(0.92_0.09_110)]',
  'bg-orange-500/15 text-orange-800 dark:bg-orange-400/20 dark:text-orange-200',
  'bg-rose-500/15 text-rose-800 dark:bg-rose-400/20 dark:text-rose-200',
  'bg-pink-500/15 text-pink-800 dark:bg-pink-400/20 dark:text-pink-200',
  'bg-fuchsia-500/15 text-fuchsia-800 dark:bg-fuchsia-400/20 dark:text-fuchsia-200',
]

// FNV-1a，只求同一个 key 每次落到同一种颜色。
function hash(s: string) {
  let h = 0x811c9dc5
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i)
    h = Math.imul(h, 0x01000193)
  }
  return h >>> 0
}

const tone = computed(() => palette[hash(props.skey) % palette.length])
const letter = computed(() => (Array.from(props.name.trim())[0] ?? '?').toUpperCase())

const box = {
  xs: 'size-5 rounded text-[11px]',
  sm: 'size-7 rounded-md text-xs',
  md: 'size-9 rounded-lg text-sm',
  lg: 'size-11 rounded-xl text-base',
}
</script>

<template>
  <span
    aria-hidden="true"
    :class="
      cn(
        'inline-flex shrink-0 items-center justify-center overflow-hidden leading-none font-semibold select-none',
        box[size],
        src ? 'bg-card ring-foreground/10 ring-1' : tone,
        disabled && 'opacity-50 grayscale',
        props.class,
      )
    "
    ><img v-if="src" :src="src" alt="" class="size-full object-cover" /><template v-else>{{
      letter
    }}</template></span
  >
</template>
