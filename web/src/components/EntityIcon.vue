<script setup lang="ts">
import type { HTMLAttributes } from 'vue'

import type { Mark } from '@/lib/icons'

import BrandIcon from './BrandIcon.vue'
import IconTile from './IconTile.vue'
import SourceAvatar from './SourceAvatar.vue'

// 按 Mark（lib/icons）显示一个对象的标识：品牌图标、来源头像或中性色图标块，尺寸三者一致。
defineProps<{
  mark: Mark
  size?: 'xs' | 'sm' | 'md' | 'lg'
  class?: HTMLAttributes['class']
}>()
</script>

<template>
  <BrandIcon v-if="'brand' in mark" :brand="mark.brand" :size="size" :class="$props.class" />
  <SourceAvatar
    v-else-if="'source' in mark"
    :name="mark.source.name"
    :skey="mark.source.key"
    :icon="mark.source.icon"
    :disabled="mark.source.enabled === false"
    :size="size"
    :class="$props.class"
  />
  <IconTile v-else :icon="mark.icon" :size="size" :class="$props.class" />
</template>
