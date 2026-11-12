<script setup lang="ts">
import { TriangleAlertIcon } from '@lucide/vue'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'

import IconTile from './IconTile.vue'

// 先弹确认框再执行的按钮，用于删除、吊销这类不可撤销的操作。
// danger 为 false 时（如恢复旧值）确认按钮用主色，否则用危险色。class、aria-label 等透传给触发按钮。
defineOptions({ inheritAttrs: false })
withDefaults(
  defineProps<{
    title: string
    description?: string
    confirmText?: string
    variant?: 'destructive' | 'outline' | 'default' | 'ghost'
    size?: 'default' | 'sm' | 'xs' | 'icon-sm'
    disabled?: boolean
    danger?: boolean
  }>(),
  { description: '', confirmText: '确定', variant: 'destructive', size: 'default', danger: true },
)
defineEmits<{ confirm: [] }>()
</script>

<template>
  <AlertDialog>
    <AlertDialogTrigger as-child>
      <Button v-bind="$attrs" :variant="variant" :size="size" :disabled="disabled"><slot /></Button>
    </AlertDialogTrigger>
    <AlertDialogContent class="sm:max-w-md">
      <AlertDialogHeader class="flex flex-row items-start gap-3 text-left">
        <IconTile :icon="TriangleAlertIcon" :tone="danger ? 'danger' : 'warning'" />
        <div class="grid gap-1">
          <AlertDialogTitle>{{ title }}</AlertDialogTitle>
          <AlertDialogDescription v-if="description">{{ description }}</AlertDialogDescription>
        </div>
      </AlertDialogHeader>
      <AlertDialogFooter>
        <AlertDialogCancel>取消</AlertDialogCancel>
        <AlertDialogAction
          :class="danger ? 'bg-destructive hover:bg-destructive/90 text-white' : ''"
          @click="$emit('confirm')"
          >{{ confirmText }}</AlertDialogAction
        >
      </AlertDialogFooter>
    </AlertDialogContent>
  </AlertDialog>
</template>
