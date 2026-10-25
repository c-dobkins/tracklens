import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

// The built dashboard is embedded in the Go binary (build tag webui), so it is
// written next to the Go code that embeds it.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
  },
  server: {
    // `npm run dev` against `tracklens -serve :8080 -file testdata/sample.json`.
    proxy: {
      '/v1': 'http://localhost:8080',
      '/healthz': 'http://localhost:8080',
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test-setup.ts'],
    include: ['src/**/*.test.{ts,tsx}'],
  },
})
