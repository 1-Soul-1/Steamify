import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    proxy: {
      // Прокси для Python (FastAPI)
      '/api': {
        target: 'http://127.0.0.1:8000',
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/api/, ''),
      },
      // Прокси для Go (WebSocket и API стрима)
      '/go': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
        rewrite: (path) => path.replace(/^\/go/, ''),
        ws: true, // Важно для WebSocket!
      },
      // Прокси для HLS видео
      '/hls': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
})