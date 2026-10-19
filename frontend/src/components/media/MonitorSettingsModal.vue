<script lang="ts">
// Exported so the parent's handlers stay in lockstep with what this modal emits.
export type MonitorSettingsPayload = {
  monitored: boolean
  monitorNewSeasons: boolean
  mediaProfileId?: number
  preferredRelease: string
  forceRelease: boolean
}
</script>

<script setup lang="ts">
import { computed, ref } from 'vue'
import BaseModal from '@/components/BaseModal.vue'
import ReleaseKeywordsInput from '@/components/media/ReleaseKeywordsInput.vue'
import type { MediaItem, MediaProfile } from '@/types/api'
import { parseReleaseKeywords } from '@/utils/releaseKeywords'

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
const preferredRelease = ref((props.item.preferredRelease ?? '').toLowerCase())
const settingsBody = ref<HTMLDivElement | null>(null)
const forceRelease = ref(props.item.forceRelease ?? true)
const hasReleaseKeywords = computed(() => parseReleaseKeywords(preferredRelease.value).length > 0)
const releaseLabel = computed(() => {
  if (!hasReleaseKeywords.value) return 'Release keywords'
  return forceRelease.value ? 'Forced releases' : 'Preferred releases'
})

// A monitored series continues to a season-selection step, mirroring the add flow.
const needsSeasonStep = computed(() => monitored.value && isSeries)

function onSave() {
  const payload: MonitorSettingsPayload = {
    monitored: monitored.value,
    monitorNewSeasons: monitorNewSeasons.value,
    mediaProfileId: mediaProfileId.value ?? undefined,
    preferredRelease: parseReleaseKeywords(preferredRelease.value).join(', '),
    forceRelease: forceRelease.value,
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
    <h3 class="text-base font-semibold text-gray-100 mb-1 flex-shrink-0">Auto-download settings</h3>
    <p class="text-xs text-gray-500 mb-4 truncate flex-shrink-0">{{ item.title }}</p>

    <div ref="settingsBody" class="min-h-0 overflow-y-auto">
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

      <!-- Release keywords and selection mode -->
      <div v-if="monitored" class="mb-4 px-1">
        <label for="release-keywords" class="block text-xs text-gray-500 mb-1.5">
          {{ releaseLabel }}
        </label>
        <ReleaseKeywordsInput
          id="release-keywords"
          v-model="preferredRelease"
          :scroll-container="settingsBody"
        />
        <div
          class="mt-3"
          :class="{ invisible: !hasReleaseKeywords }"
          :aria-hidden="!hasReleaseKeywords"
          :inert="!hasReleaseKeywords"
        >
          <div class="flex items-center gap-3">
            <button
              type="button"
              role="switch"
              :aria-checked="forceRelease"
              aria-labelledby="force-release-label"
              aria-describedby="release-mode-help"
              class="relative w-9 h-5 rounded-full transition-colors duration-200 flex-shrink-0 cursor-pointer"
              :class="forceRelease ? 'bg-emerald-600' : 'bg-gray-600'"
              @click="forceRelease = !forceRelease"
            >
              <span
                class="absolute top-0.5 left-0.5 w-4 h-4 bg-white rounded-full transition-transform duration-200"
                :class="forceRelease ? 'translate-x-4' : ''"
              />
            </button>
            <span id="force-release-label" class="text-sm text-gray-300">Only matching releases</span>
          </div>
          <p id="release-mode-help" class="text-xs text-gray-500 mt-1.5">
            {{ forceRelease
              ? 'Auto-download only releases matching at least one keyword. If none match, wait for a matching release.'
              : 'Prefer matching releases when available; otherwise auto-download the best-ranked release.' }}
          </p>
        </div>
        <p class="text-xs text-gray-500 mt-1.5">
          Comma-separated, case-insensitive keywords. Previously saved keywords are suggested as you
          type. Leave empty for no restriction or preference. Manual downloads are unaffected.
        </p>
      </div>
    </div>

    <!-- Actions -->
    <div class="flex gap-3 mt-2 flex-shrink-0">
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
