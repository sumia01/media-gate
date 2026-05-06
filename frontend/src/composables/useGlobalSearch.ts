import { readonly, ref } from 'vue'

const searchOpen = ref(false)
const activeLibraryId = ref<number | null>(null)
const searchMediaType = ref<'movie' | 'series'>('movie')

export function useGlobalSearch() {
  function openSearch() {
    searchOpen.value = true
  }

  function closeSearch() {
    searchOpen.value = false
  }

  function setActiveLibrary(id: number, mediaType: 'movie' | 'series') {
    activeLibraryId.value = id
    searchMediaType.value = mediaType
  }

  function clearActiveLibrary() {
    activeLibraryId.value = null
  }

  return {
    searchOpen: readonly(searchOpen),
    activeLibraryId: readonly(activeLibraryId),
    searchMediaType,
    openSearch,
    closeSearch,
    setActiveLibrary,
    clearActiveLibrary,
  }
}
