import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  // The repository keeps a single .env next to .env.example at the root, so
  // VITE_API_BASE_URL is read from there instead of apps/console/.env.
  envDir: '../../',
  test: {
    environment: 'jsdom',
    setupFiles: './src/tests/setup.ts',
  },
})

