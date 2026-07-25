import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { tanstackRouter } from '@tanstack/router-plugin/vite'
import path from 'path'

export default defineConfig({
  plugins: [
    tailwindcss(),
    // Must run before the React plugin: generates src/routeTree.gen.ts from src/routes/.
    tanstackRouter({ target: 'react', autoCodeSplitting: true }),
    react(),
  ],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: Number(process.env.VITE_PORT ?? 5173),
    proxy: (() => {
      const master = `http://localhost:${process.env.ORKESTRA_UI_PORT ?? 8080}`
      return {
        '/orkestra.v1': { target: master, changeOrigin: true },
        '/api':         { target: master, changeOrigin: true },
        '/auth':        { target: master, changeOrigin: true },
      }
    })(),
  },
})
