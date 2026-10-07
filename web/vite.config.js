import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// Hasil build masuk ke web/dist lalu di-embed ke binary Go (lihat web/embed.go).
export default defineConfig({
  plugins: [vue()],
  build: { outDir: 'dist', assetsDir: 'assets', emptyOutDir: true, sourcemap: false, target: 'es2020' },
  server: {
    port: 5173,
    // Saat `bun run dev`, API dan WebSocket diteruskan ke server Go di port 8868.
    proxy: {
      '/api': 'http://localhost:8868',
      '/auth': 'http://localhost:8868',
      '/ws': { target: 'ws://localhost:8868', ws: true },
    },
  },
})
