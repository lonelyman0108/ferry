<script setup lang="ts">
import { ArrowLeftIcon } from '@lucide/vue'
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { Button } from '@/components/ui/button'
import { navFor } from '@/lib/nav'

import IconTile from './IconTile.vue'

// 每个页面顶部的标题区：功能区图标 + 标题 + 一句话说明，右侧放操作按钮。
// 不传 title / description 时取侧边栏里这个功能区的名称和说明；传了 back 就显示返回按钮。
// 窄屏时标题独占一行，操作区换到下一行，标题不会被按钮挤窄。
const props = defineProps<{ title?: string; description?: string; back?: string }>()

const route = useRoute()
const router = useRouter()
const nav = computed(() => navFor(route.path))
</script>

<template>
  <div class="mb-6 flex flex-wrap items-center gap-x-4 gap-y-3">
    <div class="flex min-w-0 basis-full items-center gap-3 sm:basis-0 sm:flex-1">
      <Button
        v-if="props.back"
        variant="ghost"
        size="icon"
        class="-ml-2 shrink-0"
        aria-label="返回"
        @click="router.push(props.back)"
        ><ArrowLeftIcon
      /></Button>
      <IconTile :icon="nav.icon" :tone="nav.tone" size="lg" class="hidden sm:inline-flex" />
      <div class="min-w-0">
        <h1 class="flex flex-wrap items-center gap-2 text-xl leading-tight font-semibold">
          <span class="truncate">{{ props.title ?? nav.label }}</span>
          <slot name="badges" />
        </h1>
        <p class="text-muted-foreground mt-0.5 text-sm">
          <slot name="description">{{ props.description ?? nav.description }}</slot>
        </p>
      </div>
    </div>
    <div v-if="$slots.actions" class="flex flex-wrap items-center gap-2">
      <slot name="actions" />
    </div>
  </div>
</template>
