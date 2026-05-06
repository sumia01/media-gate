import { createApp } from 'vue'
import App from './App.vue'
import router from './router'
import './style.css'
import { configureAuth } from './api/client'
import { useAuth } from './composables/useAuth'

const { getAccessToken, refresh, logout } = useAuth()

configureAuth(getAccessToken, refresh, () => {
  // Don't redirect to login during setup — the wizard handles its own auth.
  if (router.currentRoute.value.name === 'setup') return
  logout()
  router.push('/login')
})

const app = createApp(App)

app.use(router)

app.mount('#app')
