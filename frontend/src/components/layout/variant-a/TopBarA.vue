<script setup lang="ts">
</script>

<template>
  <header class="h-16 flex items-center gap-4 px-6 flex-shrink-0 sticky top-0 z-10 bg-[#0c0f1a]/80 backdrop-blur-md border-b border-violet-900/20">
    <!-- Search -->
    <div class="flex-1 max-w-2xl">
      <div class="relative flex items-center border rounded-lg bg-[#161b2e] border-violet-800/30 focus-within:border-violet-500/50 transition-colors duration-200">
        <span class="pl-3 text-gray-500 text-sm">⌕</span>
        <input
          type="text"
          placeholder="Search movies & TV..."
          class="w-full bg-transparent py-2.5 px-3 text-sm text-gray-300 placeholder-gray-600 outline-none"
        />
      </div>
    </div>

    <div class="flex items-center gap-3 ml-auto">
      <button class="relative p-2 rounded-lg text-gray-500 hover:text-violet-300 transition-colors duration-200">
        <span class="text-lg">🔔</span>
        <span class="absolute top-1 right-1 w-2 h-2 rounded-full bg-red-500" />
      </button>
      <div class="w-8 h-8 rounded-full bg-gray-700 flex items-center justify-center text-xs text-gray-300 cursor-pointer">U</div>
    </div>
  </header>
</template>
