import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Dev server proxies API + WebSocket traffic to the FastAPI backend (port 24749).
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    rollupOptions: {
      output: {
        // Split heavy vendor libs out of the main bundle so no single chunk
        // trips the 500 kB warning (react-flow + markdown are the big ones).
        manualChunks: {
          flow: ['@xyflow/react'],
          markdown: ['react-markdown', 'remark-gfm'],
        },
      },
    },
  },
  server: {
    proxy: {
      '/api': 'http://localhost:24749',
      '/ws': { target: 'ws://localhost:24749', ws: true },
    },
  },
})
