<script setup lang="ts">
import { LoaderCircleIcon } from '@lucide/vue'
import type { Component } from 'vue'

import { Button } from '@/components/ui/button'
import type { Tone } from '@/lib/tone'

import DialogShell from './DialogShell.vue'

// 新建 / 编辑用的表单弹窗，外壳见 DialogShell。表单内容放默认插槽，回车或点保存都触发 submit。
// 对象有启停状态时绑定 v-model:enabled，开关显示在头部右侧，随保存一起提交。
// footer-start 插槽放删除、导出这类次要操作，底部栏右侧固定是取消和保存。
// 保存按钮不因为必填项没填而置灰：在 submit 的处理里用 useFormCheck（lib/form.ts）就地标出缺的项。
const open = defineModel<boolean>('open', { required: true })
const enabled = defineModel<boolean | undefined>('enabled', { default: undefined })
withDefaults(
  defineProps<{
    title: string
    description?: string
    icon?: Component
    brand?: string
    tone?: Tone
    busy?: boolean
    submitText?: string
    size?: 'sm' | 'md' | 'lg'
  }>(),
  {
    description: '',
    icon: undefined,
    brand: undefined,
    tone: 'neutral',
    submitText: '保存',
    size: 'md',
  },
)
defineEmits<{ submit: [] }>()
</script>

<template>
  <DialogShell
    v-model:enabled="enabled"
    :open="open"
    :title="title"
    :description="description"
    :icon="icon"
    :brand="brand"
    :tone="tone"
    :size="size"
    @close="open = false"
  >
    <form class="grid content-start gap-6" @submit.prevent="$emit('submit')">
      <slot />
      <button type="submit" class="hidden" />
    </form>
    <template v-if="$slots['footer-start']" #footer-start><slot name="footer-start" /></template>
    <template #footer>
      <Button variant="outline" @click="open = false">取消</Button>
      <Button :disabled="busy" @click="$emit('submit')">
        <LoaderCircleIcon v-if="busy" class="animate-spin" />
        {{ submitText }}
      </Button>
    </template>
  </DialogShell>
</template>
