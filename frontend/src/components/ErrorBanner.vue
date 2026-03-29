<script setup lang="ts">
defineProps<{
  message: string
}>()
</script>

<template>
  <div
    v-if="message"
    class="mb-4 px-4 py-3 rounded-lg bg-red-500/10 border border-red-500/30 text-red-400 text-sm"
  >
    {{ message }}
  </div>
</template>
