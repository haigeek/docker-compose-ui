import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// https://vitejs.dev/config/
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
  build: {
    rollupOptions: {
      output: {
        // 代码分割优化
        manualChunks: {
          'vue-vendor': ['vue'],
          'monaco-editor': ['monaco-editor', '@monaco-editor/loader'],
          'prettier': ['prettier/standalone', 'prettier/plugins/yaml', 'prettier/plugins/estree'],
        },
      },
    },
    // 启用 Gzip 压缩报告
    reportCompressedSize: true,
    // Chunk 大小警告限制
    chunkSizeWarningLimit: 500,
  },
})
