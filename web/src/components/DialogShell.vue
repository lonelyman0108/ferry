<script setup lang="ts">
import { XIcon } from '@lucide/vue'
import type { Component } from 'vue'

import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import type { Tone } from '@/lib/tone'
import { cn } from '@/lib/utils'

import BrandIcon from './BrandIcon.vue'
import IconTile from './IconTile.vue'

// 全站弹窗的外壳，来源编辑器、表单弹窗、详情弹窗都用它，保证头部和底部栏一致：
// 头部是图标、标题（badges 插槽放状态徽章）、说明，右侧依次是 header-actions 插槽、启用开关、关闭按钮；
// 正文在内部滚动，默认带内边距，bare 时交给调用方自己排（如来源编辑器的分栏）；
// 底部栏左侧是 footer-start 插槽（导出、删除这类次要操作），右侧是 footer 插槽（取消、保存）。
// 关闭按钮、Esc 只发 close，由调用方决定要不要真的关（例如有未保存的修改时先确认）；
// 点遮罩不关闭，免得误触丢掉填了一半的内容。
const enabled = defineModel<boolean | undefined>('enabled', { default: undefined })
withDefaults(
  defineProps<{
    open: boolean
    title: string
    description?: string
    icon?: Component
    // 品牌图标名（lib/brands），优先于 icon
    brand?: string
    tone?: Tone
    size?: 'sm' | 'md' | 'lg' | 'xl' | 'full'
    bare?: boolean
  }>(),
  { description: '', icon: undefined, brand: undefined, tone: 'primary', size: 'md' },
)
const emit = defineEmits<{ close: [] }>()

const width = {
  sm: 'sm:max-w-md',
  md: 'sm:max-w-xl',
  lg: 'sm:max-w-3xl',
  xl: 'sm:max-w-5xl',
  full: 'h-[94dvh] sm:max-w-[min(96vw,88rem)]',
}
</script>

<template>
  <Dialog :open="open" @update:open="(v) => !v && emit('close')">
    <DialogContent
      :show-close-button="false"
      :class="
        cn(
          'flex flex-col gap-0 overflow-hidden p-0',
          size === 'full' ? '' : 'max-h-[min(90dvh,56rem)]',
          width[size],
        )
      "
      @interact-outside.prevent
      @open-auto-focus.prevent
    >
      <!-- 头部不换行：窄屏时隐藏图标，标题截断，开关和关闭始终在右侧同一行 -->
      <div class="flex items-center gap-3 border-b px-4 py-3 sm:px-5">
        <BrandIcon v-if="brand" :brand="brand" class="max-sm:hidden" />
        <IconTile v-else-if="icon" :icon="icon" :tone="tone" class="max-sm:hidden" />
        <div class="mr-auto min-w-0">
          <DialogTitle class="flex flex-wrap items-center gap-2 text-base">
            <span class="truncate">{{ title }}</span>
            <slot name="badges" />
          </DialogTitle>
          <DialogDescription v-if="description || $slots.description" class="truncate">
            <slot name="description">{{ description }}</slot>
          </DialogDescription>
        </div>
        <div class="flex shrink-0 items-center gap-3">
          <slot name="header-actions" />
          <Label v-if="enabled !== undefined" class="font-normal"
            ><Switch v-model="enabled" /> 启用</Label
          >
          <Button variant="ghost" size="icon-sm" aria-label="关闭" @click="emit('close')">
            <XIcon />
          </Button>
        </div>
      </div>

      <div v-if="bare" class="min-h-0 flex-1"><slot /></div>
      <div v-else class="min-h-0 flex-1 overflow-y-auto p-4 sm:p-5"><slot /></div>

      <div
        v-if="$slots.footer || $slots['footer-start']"
        class="bg-muted/30 flex items-center gap-2 border-t px-4 py-3 sm:px-5"
      >
        <div class="mr-auto flex items-center gap-2"><slot name="footer-start" /></div>
        <slot name="footer" />
      </div>
    </DialogContent>
  </Dialog>
</template>
