import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  server: {
    port: 9227,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8227',
        changeOrigin: true,
      },
      '/health': {
        target: 'http://127.0.0.1:8227',
        changeOrigin: true,
      },
    },
  },
})
