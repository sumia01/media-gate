<script setup lang="ts">
defineProps<{
  maxWidth?: string
}>()

const emit = defineEmits<{
  close: []
}>()
</script>

<template>
  <Teleport to="body">
    <div
      class="fixed inset-0 z-50 flex items-center justify-center"
    >
      <div class="absolute inset-0 bg-black/60" @click="emit('close')"></div>
      <div
        class="relative bg-[#0f1225] border border-violet-900/30 rounded-xl p-6 shadow-2xl w-full"
        :class="maxWidth ?? 'max-w-sm'"
      >
        <slot />
      </div>
    </div>
  </Teleport>
</template>
