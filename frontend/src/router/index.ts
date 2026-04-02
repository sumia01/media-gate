import { createRouter, createWebHistory } from 'vue-router'
import { useAuth } from '@/composables/useAuth'
import LoginView from '@/views/LoginView.vue'
import LayoutA from '@/components/layout/variant-a/LayoutA.vue'
import HomeView from '@/views/HomeView.vue'
import LibrariesView from '@/views/LibrariesView.vue'
import LibraryDetailView from '@/views/LibraryDetailView.vue'
import MediaDetailView from '@/views/MediaDetailView.vue'
import MediaPreviewView from '@/views/MediaPreviewView.vue'
import MediaProfilesView from '@/views/MediaProfilesView.vue'
import IndexersView from '@/views/IndexersView.vue'
import IndexerSearchView from '@/views/IndexerSearchView.vue'
import SettingsView from '@/views/SettingsView.vue'
import UserProfileView from '@/views/UserProfileView.vue'
import UsersView from '@/views/UsersView.vue'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/login',
      name: 'login',
      component: LoginView,
      meta: { public: true },
    },
    {
      path: '/setup',
      name: 'setup',
      component: () => import('@/views/SetupView.vue'),
      meta: { public: true },
    },
    {
      path: '/',
      component: LayoutA,
      children: [
        {
          path: '',
          name: 'home',
          component: HomeView,
        },
        {
          path: 'libraries',
          name: 'libraries',
          component: LibrariesView,
        },
        {
          path: 'library/:id',
          name: 'library-detail',
          component: LibraryDetailView,
          props: true,
        },
        {
          path: 'media/:id',
          name: 'media-detail',
          component: MediaDetailView,
          props: true,
        },
        {
          path: 'search/:source/:externalId',
          name: 'media-preview',
          component: MediaPreviewView,
          props: true,
        },
        {
          path: 'media-profiles',
          name: 'media-profiles',
          component: MediaProfilesView,
        },
        {
          path: 'indexers',
          name: 'indexers',
          component: IndexersView,
        },
        {
          path: 'indexers/search',
          name: 'indexer-search',
          component: IndexerSearchView,
        },
        {
          path: 'settings',
          name: 'settings',
          component: SettingsView,
        },
        {
          path: 'profile',
          name: 'profile',
          component: UserProfileView,
        },
        {
          path: 'users',
          name: 'users',
          component: UsersView,
        },
      ],
    },
  ],
})

router.beforeEach(async (to) => {
  // Always allow access to the setup page itself.
  if (to.name === 'setup') return true

  const { isAuthenticated, refresh, fetchProfile, getSetupStatus } = useAuth()

  // Check onboarding status — redirect to wizard if not completed.
  try {
    const status = await getSetupStatus()
    if (status.needsSetup || !status.onboardingCompleted) {
      return { name: 'setup' }
    }
  } catch {
    // If status check fails, continue with normal auth flow.
  }

  // Public routes (login) don't need auth.
  if (to.meta.public) return true

  if (isAuthenticated.value) return true

  // Try refreshing the token (cookie might still be valid).
  const ok = await refresh()
  if (ok) {
    try {
      await fetchProfile()
    } catch {
      // Profile fetch failed — token might be invalid.
      return { name: 'login', query: { redirect: to.fullPath } }
    }
    return true
  }

  return { name: 'login', query: { redirect: to.fullPath } }
})

export default router
