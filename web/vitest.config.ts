import { defineConfig } from 'vitest/config'
import path from 'path'

// Standalone from vite.config.ts on purpose: the unit tests cover pure helpers in
// src/lib/, so none of the app plugins (Tailwind, the TanStack Router codegen, the
// React refresh transform) need to run. Keeping them out makes the suite start fast
// and stops a test run from regenerating src/routeTree.gen.ts as a side effect.
export default defineConfig({
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts'],
  },
})
