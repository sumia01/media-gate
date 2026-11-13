<script setup lang="ts">
import { Eye, X } from 'lucide-vue-next'
import { onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import client from '@/api/client'
import ErrorBanner from '@/components/ErrorBanner.vue'
import DiscoverResultsStatus from '@/components/media/DiscoverResultsStatus.vue'
import { discoverKey } from '@/composables/useDiscoverFilter'
import { usePagedDiscover } from '@/composables/usePagedDiscover'
import { useWatchedPosters } from '@/composables/useWatchedPosters'
import type { WatchedItem } from '@/types/api'

const router = useRouter()
const error = ref('')
const removed = ref(new Set<string>())
const deleting = ref(new Set<number>())
const deleteController = new AbortController()
const { itemPosterUrl, loadMissingPosters, onPosterError } = useWatchedPosters()
let nextCursor: string | undefined

const { items, loading, initialLoading, loadFailed, hasMore, sentinel, fetchPage } = usePagedDiscover<WatchedItem>(
  async (page, signal) => {
    const before = page === 1 ? undefined : nextCursor
    const { data } = await client.GET('/watched', {
      params: { query: { limit: 30, before } },
      signal,
    })
    if (signal.aborted) return
    if (
      !data ||
      typeof data.hasMore !== 'boolean' ||
      (data.hasMore && (!data.nextCursor || data.nextCursor === before))
    ) {
      throw new Error('Invalid watched page')
    }
    nextCursor = data.nextCursor
    return { items: data.items, page, totalPages: data.hasMore ? page + 1 : page }
  },
  (item) => String(item.id),
  {
    include: (item) => !removed.value.has(discoverKey(item)),
    filterKey: () => removed.value.size,
    maxPages: Number.POSITIVE_INFINITY,
  },
)

watch(items, loadMissingPosters)
onUnmounted(() => deleteController.abort())

async function unmark(item: WatchedItem) {
  if (deleting.value.has(item.id)) return
  deleting.value.add(item.id)
  error.value = ''
  try {
    const { error: err } = await client.DELETE('/watched/{id}', {
      params: { path: { id: item.id } },
      signal: deleteController.signal,
    })
    if (deleteController.signal.aborted) return
    if (err) throw new Error('Failed to remove watched item')
    removed.value.add(discoverKey(item))
  } catch {
    if (!deleteController.signal.aborted) error.value = 'Failed to remove watched item. Please try again.'
  } finally {
    deleting.value.delete(item.id)
  }
}

function navigate(item: WatchedItem) {
  router.push({
    name: 'media-preview',
    params: { source: item.source, externalId: String(item.externalId) },
    query: { mediaType: item.mediaType },
  })
}
</script>

<template>
  <div>
    <h1 class="text-xl font-semibold text-gray-100 tracking-tight mb-6">Watched</h1>

    <ErrorBanner :message="error" />

    <div v-if="initialLoading" role="status" class="text-gray-500 text-sm">Loading...</div>

    <div v-else class="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5 xl:grid-cols-6 gap-4">
      <div
        v-for="item in items"
        :key="item.id"
        class="group relative rounded-lg overflow-hidden bg-[#161b2e] border border-violet-900/20 cursor-pointer hover:border-violet-500/40 transition-all duration-200"
        @click="navigate(item)"
      >
        <!-- Poster -->
        <div class="aspect-[2/3] bg-gradient-to-br from-violet-900/20 to-fuchsia-900/20 flex items-center justify-center">
          <img
            v-if="itemPosterUrl(item)"
            :src="itemPosterUrl(item)!"
            :alt="item.title"
            class="w-full h-full object-cover"
            loading="lazy"
            @error="onPosterError(item)"
          />
          <span v-else class="text-4xl text-gray-600">
            {{ item.mediaType === 'movie' ? '&#127910;' : '&#128250;' }}
          </span>
        </div>

        <!-- Info -->
        <div class="p-2.5">
          <p class="text-sm font-medium text-gray-200 truncate">{{ item.title }}</p>
          <div class="flex items-center gap-2 mt-1">
            <span v-if="item.year" class="text-xs text-gray-500">{{ item.year }}</span>
            <span
              class="text-[9px] font-bold uppercase px-1.5 py-0.5 rounded-full"
              :class="item.mediaType === 'movie'
                ? 'bg-violet-600/20 text-violet-300'
                : 'bg-fuchsia-600/20 text-fuchsia-300'"
            >
              {{ item.mediaType }}
            </span>
          </div>
        </div>

        <!-- Unmark button (hover overlay) -->
        <button
          class="absolute top-2 right-2 p-1.5 rounded-lg bg-black/60 text-gray-400 hover:text-red-400 opacity-0 group-hover:opacity-100 transition-opacity duration-200"
          title="Remove from watched"
          :disabled="deleting.has(item.id)"
          @click.stop="unmark(item)"
        >
          <X class="w-4 h-4" />
        </button>

        <!-- Watched badge -->
        <div class="absolute top-2 left-2 flex items-center gap-1 px-1.5 py-0.5 rounded-full bg-emerald-600/80 text-white text-[9px] font-medium">
          <Eye class="w-3 h-3" />
          Watched
        </div>
      </div>
    </div>

    <div ref="sentinel">
      <DiscoverResultsStatus
        :ready="true"
        :loading="loading"
        :initial-loading="initialLoading"
        :load-failed="loadFailed"
        :has-more="hasMore"
        :item-count="items.length"
        :hidden-count="0"
        empty-title="No watched items yet"
        empty-message="Mark media as watched from the media detail or preview pages."
        @load-more="fetchPage"
      />
    </div>
  </div>
</template>
