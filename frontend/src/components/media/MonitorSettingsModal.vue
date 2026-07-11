<script lang="ts">
// Exported so the parent's handlers stay in lockstep with what this modal emits.
export type MonitorSettingsPayload = {
  monitored: boolean
  monitorNewSeasons: boolean
  mediaProfileId?: number
  preferredRelease: string
}
</script>

<script setup lang="ts">
import { computed, ref } from 'vue'
import BaseModal from '@/components/BaseModal.vue'
import type { MediaItem, MediaProfile } from '@/types/api'

const props = defineProps<{
  item: MediaItem
  profiles: MediaProfile[]
}>()

const emit = defineEmits<{
  close: []
  save: [payload: MonitorSettingsPayload]
  // Emitted instead of `save` when a season-selection step should follow
  // (monitored series): the parent opens the season monitor modal.
  next: [payload: MonitorSettingsPayload]
}>()

const isSeries = props.item.mediaType === 'series'

const monitored = ref(props.item.monitored ?? false)
const monitorNewSeasons = ref(props.item.monitorNewSeasons ?? true)
const mediaProfileId = ref<number | null>(props.item.mediaProfileId ?? null)
const preferredRelease = ref(props.item.preferredRelease ?? '')

// A monitored series continues to a season-selection step, mirroring the add flow.
const needsSeasonStep = computed(() => monitored.value && isSeries)

function onSave() {
  const payload: MonitorSettingsPayload = {
    monitored: monitored.value,
    monitorNewSeasons: monitorNewSeasons.value,
    mediaProfileId: mediaProfileId.value ?? undefined,
    preferredRelease: preferredRelease.value.trim(),
  }
  if (needsSeasonStep.value) {
    emit('next', payload)
  } else {
    emit('save', payload)
  }
}
</script>

<template>
  <BaseModal max-width="max-w-md" @close="emit('close')">
    <h3 class="text-base font-semibold text-gray-100 mb-1">Auto-download settings</h3>
    <p class="text-xs text-gray-500 mb-4 truncate">{{ item.title }}</p>

    <!-- Auto-download toggle -->
    <div class="mb-3 px-1">
      <label class="flex items-center gap-3 cursor-pointer">
        <button
          type="button"
          class="relative w-9 h-5 rounded-full transition-colors duration-200 flex-shrink-0"
          :class="monitored ? 'bg-emerald-600' : 'bg-gray-600'"
          @click="monitored = !monitored"
        >
          <span
            class="absolute top-0.5 left-0.5 w-4 h-4 bg-white rounded-full transition-transform duration-200"
            :class="monitored ? 'translate-x-4' : ''"
          />
        </button>
        <span class="text-sm text-gray-300">Auto-download</span>
      </label>
    </div>

    <!-- Monitor new seasons (series only) -->
    <div v-if="monitored && isSeries" class="mb-3 px-1">
      <label class="flex items-center gap-3 cursor-pointer">
        <button
          type="button"
          class="relative w-9 h-5 rounded-full transition-colors duration-200 flex-shrink-0"
          :class="monitorNewSeasons ? 'bg-emerald-600' : 'bg-gray-600'"
          @click="monitorNewSeasons = !monitorNewSeasons"
        >
          <span
            class="absolute top-0.5 left-0.5 w-4 h-4 bg-white rounded-full transition-transform duration-200"
            :class="monitorNewSeasons ? 'translate-x-4' : ''"
          />
        </button>
        <span class="text-sm text-gray-300">Monitor new seasons</span>
      </label>
    </div>

    <!-- Quality profile -->
    <div v-if="monitored && profiles.length" class="mb-4 px-1">
      <label class="block text-xs text-gray-500 mb-1.5">Quality Profile</label>
      <select
        class="w-full text-sm bg-[#161b2e] border border-violet-900/20 rounded-lg px-3 py-2 text-gray-200 focus:outline-none focus:border-violet-500/50"
        :value="mediaProfileId ?? ''"
        @change="mediaProfileId = ($event.target as HTMLSelectElement).value ? Number(($event.target as HTMLSelectElement).value) : null"
      >
        <option value="">None</option>
        <option v-for="p in profiles" :key="p.id" :value="p.id">{{ p.name }}</option>
      </select>
    </div>

    <!-- Preferred release -->
    <div v-if="monitored" class="mb-4 px-1">
      <label class="block text-xs text-gray-500 mb-1.5">Preferred release</label>
      <input
        v-model="preferredRelease"
        type="text"
        placeholder="e.g. ETHEL, FLUX"
        class="w-full text-sm bg-[#161b2e] border border-violet-900/20 rounded-lg px-3 py-2 text-gray-200 placeholder-gray-600 focus:outline-none focus:border-violet-500/50"
      />
      <p class="text-xs text-gray-500 mt-1.5">
        Comma-separated keywords matched against the release title. Matching releases are preferred
        when available; otherwise the best-ranked release is still grabbed. Leave empty for no
        preference.
      </p>
    </div>

    <!-- Actions -->
    <div class="flex gap-3 mt-2">
      <button
        type="button"
        class="flex-1 px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white text-sm font-medium transition-colors duration-200"
        @click="onSave"
      >
        {{ needsSeasonStep ? 'Next: seasons' : 'Save' }}
      </button>
      <button
        type="button"
        class="px-4 py-2 rounded-lg border border-gray-700/50 text-gray-400 hover:text-gray-300 text-sm transition-colors duration-200"
        @click="emit('close')"
      >
        Cancel
      </button>
    </div>
  </BaseModal>
</template>
