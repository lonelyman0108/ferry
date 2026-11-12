<script setup lang="ts">
import { PencilIcon, Trash2Icon } from '@lucide/vue'

import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'

import ConfirmButton from './ConfirmButton.vue'

// 列表行右侧的操作区，各页面统一用它：具名操作（默认插槽，描边小按钮）→ 编辑 → 删除 → 启用开关。
// 编辑、删除只有图标；启用开关固定在最右，切换即保存。不传 enabled 时不显示开关，
// 不传 deleteTitle 时不显示删除。点击不冒泡，可以放在整行可点的卡片里。
withDefaults(
  defineProps<{
    enabled?: boolean
    toggling?: boolean
    editable?: boolean
    deleteTitle?: string
    deleteDescription?: string
  }>(),
  {
    enabled: undefined,
    toggling: false,
    editable: true,
    deleteTitle: '',
    deleteDescription: '此操作不可撤销。',
  },
)
defineEmits<{ edit: []; remove: []; toggle: [enabled: boolean] }>()
</script>

<template>
  <div class="flex shrink-0 items-center gap-1" @click.stop @keydown.stop>
    <slot />
    <Button
      v-if="editable"
      variant="ghost"
      size="icon-sm"
      aria-label="编辑"
      title="编辑"
      @click="$emit('edit')"
      ><PencilIcon
    /></Button>
    <ConfirmButton
      v-if="deleteTitle"
      :title="deleteTitle"
      :description="deleteDescription"
      confirm-text="删除"
      variant="ghost"
      size="icon-sm"
      aria-label="删除"
      class="text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
      @confirm="$emit('remove')"
    >
      <Trash2Icon />
    </ConfirmButton>
    <template v-if="enabled !== undefined">
      <span
        v-if="$slots.default || editable || deleteTitle"
        class="bg-border mx-1.5 h-5 w-px"
        aria-hidden="true"
      />
      <Switch
        :model-value="enabled"
        :disabled="toggling"
        :aria-label="enabled ? '已启用' : '已停用'"
        :title="enabled ? '已启用，点击停用' : '已停用，点击启用'"
        @update:model-value="(v: boolean) => $emit('toggle', v)"
      />
    </template>
  </div>
</template>
