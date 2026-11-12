<script setup lang="ts">
import { PencilIcon, Trash2Icon } from '@lucide/vue'

import { Button } from '@/components/ui/button'
import { Switch } from '@/components/ui/switch'

import ConfirmButton from './ConfirmButton.vue'

// 列表行右侧的操作区，各页面统一用它：具名操作（默认插槽，描边小按钮）→ 编辑 → 删除 → 启用开关。
// 编辑、删除只有图标；启用开关固定在最右，切换即保存。不传 enabled 时不显示开关，
// 不传 deleteTitle 时不显示删除；deleteBlocked 是暂时不能删除的原因，给了时删除按钮置灰，悬停可见原因。
// 点击、回车、空格不冒泡，可以放在整行可点的卡片里；
// Esc 照常冒泡，弹窗和菜单靠它关闭。name 是这一行对象的名称，写进各按钮的读屏名称，
// 否则一页里几个开关都只读出「启用」，分不清是哪一项。
const props = withDefaults(
  defineProps<{
    name?: string
    enabled?: boolean
    toggling?: boolean
    editable?: boolean
    deleteTitle?: string
    deleteDescription?: string
    deleteBlocked?: string
  }>(),
  {
    name: '',
    enabled: undefined,
    toggling: false,
    editable: true,
    deleteTitle: '',
    deleteDescription: '此操作不可撤销。',
    deleteBlocked: '',
  },
)
defineEmits<{ edit: []; remove: []; toggle: [enabled: boolean] }>()

const named = (action: string) => (props.name ? `${action}「${props.name}」` : action)
</script>

<template>
  <div class="flex shrink-0 items-center gap-1" @click.stop @keydown.enter.stop @keydown.space.stop>
    <slot />
    <Button
      v-if="editable"
      variant="ghost"
      size="icon-sm"
      :aria-label="named('编辑')"
      title="编辑"
      @click="$emit('edit')"
      ><PencilIcon
    /></Button>
    <!-- 置灰的按钮收不到鼠标事件，原因写在外层的 title 上 -->
    <span v-if="deleteTitle && deleteBlocked" class="inline-flex" :title="deleteBlocked">
      <Button
        variant="ghost"
        size="icon-sm"
        disabled
        :aria-label="`${named('删除')}：${deleteBlocked}`"
        class="text-muted-foreground"
        ><Trash2Icon
      /></Button>
    </span>
    <ConfirmButton
      v-else-if="deleteTitle"
      :title="deleteTitle"
      :description="deleteDescription"
      confirm-text="删除"
      variant="ghost"
      size="icon-sm"
      :aria-label="named('删除')"
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
        :aria-label="named('启用')"
        :title="enabled ? '已启用，点击停用' : '已停用，点击启用'"
        @update:model-value="(v: boolean) => $emit('toggle', v)"
      />
    </template>
  </div>
</template>
