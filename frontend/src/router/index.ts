import { createRouter, createWebHistory } from 'vue-router'
import HomeView from '@/views/HomeView.vue'
import LibrariesView from '@/views/LibrariesView.vue'
import LibraryDetailView from '@/views/LibraryDetailView.vue'
import MediaDetailView from '@/views/MediaDetailView.vue'
import SettingsView from '@/views/SettingsView.vue'

const router = createRouter({
  history: createWebHistory(import.meta.env.BASE_URL),
  routes: [
    {
      path: '/',
      name: 'home',
      component: HomeView,
    },
    {
      path: '/libraries',
      name: 'libraries',
      component: LibrariesView,
    },
    {
      path: '/library/:id',
      name: 'library-detail',
      component: LibraryDetailView,
      props: true,
    },
    {
      path: '/media/:id',
      name: 'media-detail',
      component: MediaDetailView,
      props: true,
    },
    {
      path: '/settings',
      name: 'settings',
      component: SettingsView,
    },
  ],
})

export default router
