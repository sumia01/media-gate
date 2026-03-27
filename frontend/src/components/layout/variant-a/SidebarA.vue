<script setup lang="ts">
import { navItems } from '../../media/dummyData'

const collapsed = defineModel<boolean>('collapsed', { default: false })
</script>

<template>
  <aside
    class="h-screen flex flex-col flex-shrink-0 bg-[#0c0f1a] border-r border-violet-900/20 transition-all duration-300"
    :class="collapsed ? 'w-16' : 'w-56'"
  >
    <!-- Brand -->
    <div class="flex items-center h-16 px-4 gap-3">
      <div class="w-8 h-8 rounded-lg flex items-center justify-center font-black text-sm text-violet-400 flex-shrink-0">
        MG
      </div>
      <span v-if="!collapsed" class="font-semibold text-sm tracking-tight text-violet-400 transition-opacity duration-200">
        Media Gate
      </span>
    </div>

    <!-- Nav -->
    <nav class="flex-1 px-2 mt-2 space-y-1">
      <button
        v-for="item in navItems"
        :key="item.label"
        class="w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-colors duration-200"
        :class="item.active
          ? 'bg-violet-600/20 text-violet-300'
          : 'text-gray-500 hover:text-violet-300 hover:bg-violet-600/10'"
      >
        <span class="text-base w-5 text-center flex-shrink-0">{{ item.icon }}</span>
        <span v-if="!collapsed" class="truncate">{{ item.label }}</span>
        <span
          v-if="item.badge && !collapsed"
          class="ml-auto text-[10px] font-bold px-1.5 py-0.5 rounded-full bg-violet-600 text-white"
        >
          {{ item.badge }}
        </span>
      </button>
    </nav>

    <!-- Collapse toggle -->
    <div class="px-2 mb-2">
      <button
        class="w-full flex items-center justify-center gap-3 px-3 py-2.5 rounded-lg text-sm text-gray-500 hover:text-violet-300 hover:bg-violet-600/10 transition-colors duration-200"
        @click="collapsed = !collapsed"
      >
        <span class="text-base transition-transform duration-300" :class="collapsed ? 'rotate-180' : ''">«</span>
        <span v-if="!collapsed">Collapse</span>
      </button>
    </div>

    <!-- User -->
    <div class="px-3 py-3 border-t border-violet-900/20">
      <div class="flex items-center gap-3">
        <div class="w-8 h-8 rounded-full bg-gray-700 flex items-center justify-center text-xs text-gray-300 flex-shrink-0">U</div>
        <div v-if="!collapsed" class="overflow-hidden">
          <p class="text-sm font-medium text-gray-300 truncate">User</p>
        </div>
      </div>
    </div>
  </aside>
</template>
