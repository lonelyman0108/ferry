<script setup lang="ts">
import { InfoIcon } from '@lucide/vue'
</script>

<template>
  <p class="text-muted-foreground flex gap-2 text-sm">
    <InfoIcon class="text-info mt-0.5 size-4 shrink-0" />
    <span><slot /></span>
  </p>
</template>
