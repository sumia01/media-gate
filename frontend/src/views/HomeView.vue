<script setup lang="ts">
import { ref, onMounted } from 'vue'
import client from '@/api/client'

const healthStatus = ref<string>('loading...')

onMounted(async () => {
  const { data, error } = await client.GET('/health')
  if (error) {
    healthStatus.value = 'error'
    return
  }
  healthStatus.value = data.status
})
</script>

<template>
  <div class="min-h-screen flex items-center justify-center bg-gray-950 text-white">
    <div class="text-center">
      <h1 class="text-4xl font-bold mb-4">Media Gate</h1>
      <p class="text-gray-400">
        Health: <span class="font-mono">{{ healthStatus }}</span>
      </p>
    </div>
  </div>
</template>
