import { writeFileSync } from 'node:fs'
import { defineConfig, type Plugin } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { mockApi } from './mock/plugin'

// Vite empties dist/ on every build; the Go embed directive needs the
// directory to exist even before the first build, so a placeholder is kept.
const keepDist = (): Plugin => ({
  name: 'keep-dist',
  closeBundle() {
    writeFileSync('dist/.gitkeep', '')
  },
})

export default defineConfig(({ mode }) => ({
  plugins: [react(), tailwindcss(), keepDist(), ...(mode === 'mock' ? [mockApi()] : [])],
  build: { outDir: 'dist', emptyOutDir: true, sourcemap: false },
  server: {
    // `npm run dev` proxies the API to the running stack (PANEL_LOCAL_PORT);
    // `npm run dev:mock` answers it with fake data instead.
    proxy: mode === 'mock' ? undefined : { '/api': 'http://127.0.0.1:28437', '/health': 'http://127.0.0.1:28437' },
  },
}))
