import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: 'dist',
    rollupOptions: {
      output: {
        manualChunks: {
          // ─── Vendor: React core (shared by all routes) ───
          'vendor-react': ['react', 'react-dom', 'react-router-dom'],
          // ─── Vendor: Markdown rendering (heavy, only needed in chat) ───
          'vendor-markdown': [
            'react-markdown',
            'remark-gfm',
            'rehype-highlight',
            'dompurify',
          ],
          // ─── Vendor: State + icons ───
          'vendor-ui': ['zustand', '@phosphor-icons/react'],
        },
      },
    },
  },
  server: {
    allowedHosts: ["op-test.avril.onl", "localhost"],
    proxy: {
      '/api': 'http://gateway:18790',
      '/ws': {
        target: 'ws://gateway:18790',
        ws: true,
      },
    },
  },
})
