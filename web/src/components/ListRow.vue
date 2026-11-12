<script setup lang="ts">
// 卡片式列表的一行：左侧图标（icon 插槽），中间标题（title、badges 插槽）和一行说明（description 插槽），
// 右侧操作（actions 插槽，一般放 RowActions）；默认插槽放标题下方的附加内容（如订阅的链接）。
// 换行按卡片自身宽度判断（容器查询），同一列表里的行宽度相同，所以要换一起换：
// 宽时操作和标题在同一行，窄时操作统一移到下一行靠右，不会出现有的行挤成两行、有的不挤。
// dim 为 true 时（停用）标题和说明变淡，操作区保持原样，方便直接点开关。
// 有详情页的行整行可点：title 插槽里的链接加 data-row-link 和 after:absolute after:inset-0，
// 它的 ::after 铺满整行；操作区叠在上面（relative z-10），不会把按钮套进链接里。悬停、键盘聚焦时整行描边。
defineProps<{ dim?: boolean }>()
</script>

<template>
  <div
    class="@container bg-card text-card-foreground ring-foreground/10 has-[[data-row-link]:hover]:ring-primary/40 has-[[data-row-link]:focus-visible]:ring-ring/50 relative rounded-xl ring-1 transition has-[[data-row-link]:focus-visible]:ring-3 has-[[data-row-link]:hover]:shadow-sm"
  >
    <div class="flex flex-col gap-3 p-4 @2xl:flex-row @2xl:items-center">
      <div class="flex min-w-0 flex-1 items-center gap-3">
        <span class="shrink-0" :class="dim ? 'opacity-50 grayscale' : ''"
          ><slot name="icon"
        /></span>
        <div class="min-w-0 flex-1" :class="dim ? 'opacity-60' : ''">
          <div class="flex flex-wrap items-center gap-2 text-sm font-medium">
            <slot name="title" />
            <slot name="badges" />
          </div>
          <div
            v-if="$slots.description"
            class="text-muted-foreground mt-1 flex flex-wrap gap-x-4 gap-y-1 text-xs"
          >
            <slot name="description" />
          </div>
        </div>
      </div>
      <div v-if="$slots.actions" class="relative z-10 flex justify-end">
        <slot name="actions" />
      </div>
    </div>
    <div v-if="$slots.default" class="px-4 pb-4"><slot /></div>
  </div>
</template>
