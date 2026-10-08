import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig, type Plugin } from 'vite'

const rootDir = path.dirname(fileURLToPath(import.meta.url))

/**
 * Vite empties `dist` on every build, but the Go binary embeds `web/dist` with
 * `//go:embed all:dist` and expects the placeholder `.gitkeep` to survive.
 */
function keepGitkeep(): Plugin {
  return {
    name: 'orxest-keep-gitkeep',
    apply: 'build',
    closeBundle() {
      const dist = path.resolve(rootDir, 'dist')
      fs.mkdirSync(dist, { recursive: true })
      const file = path.join(dist, '.gitkeep')
      if (!fs.existsSync(file)) {
        fs.writeFileSync(file, '')
      }
    },
  }
}

// The dev server proxies /api to the Go backend. SSE streams are proxied too:
// the backend sets the correct `text/event-stream` headers.
export default defineConfig({
  plugins: [react(), tailwindcss(), keepGitkeep()],
  resolve: {
    alias: { '@': path.resolve(rootDir, 'src') },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8787',
        changeOrigin: true,
      },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
  },
})
