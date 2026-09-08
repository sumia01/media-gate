import { fileURLToPath, URL } from 'node:url'

import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import vueDevTools from 'vite-plugin-vue-devtools'
import tailwindcss from '@tailwindcss/vite'

const configuredPort = process.env.VITE_PORT
const port = configuredPort === undefined ? undefined : Number(configuredPort)
if (port !== undefined && (!Number.isInteger(port) || port < 1 || port > 65535)) {
  throw new Error(`invalid VITE_PORT: ${configuredPort}`)
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    vue(),
    vueDevTools(),
    tailwindcss(),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url))
    },
  },
  server: {
    host: process.env.VITE_HOST,
    port,
    strictPort: port !== undefined,
    proxy: {
      '/api': process.env.VITE_API_PROXY_TARGET ?? 'http://localhost:8080',
    },
  },
})
