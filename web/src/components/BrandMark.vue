<script setup lang="ts">
import { ShipIcon } from '@lucide/vue'
import type { HTMLAttributes } from 'vue'

import { cn } from '@/lib/utils'

const props = defineProps<{ class?: HTMLAttributes['class'] }>()
</script>

<template>
  <span
    :class="
      cn(
        'bg-primary text-primary-foreground inline-flex size-8 shrink-0 items-center justify-center rounded-lg shadow-sm [&>svg]:size-4.5',
        props.class,
      )
    "
    ><ShipIcon
  /></span>
</template>
