import { ref, readonly } from 'vue'

const searchOpen = ref(false)

export function useGlobalSearch() {
  function openSearch() {
    searchOpen.value = true
  }

  function closeSearch() {
    searchOpen.value = false
  }

  return {
    searchOpen: readonly(searchOpen),
    openSearch,
    closeSearch,
  }
}
