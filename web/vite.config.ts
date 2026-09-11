import { svelte } from '@sveltejs/vite-plugin-svelte'
import { VitePWA } from 'vite-plugin-pwa'
import { defineConfig } from 'vitest/config'

// https://vite.dev/config/
export default defineConfig({
  plugins: [
    svelte(),
    // Phase 7 (spec §6): installable PWA. The app shell is precached and
    // served by the service worker; /api responses are never cached —
    // the local IndexedDB cache is the offline source of truth.
    VitePWA({
      registerType: 'autoUpdate',
      includeAssets: ['favicon.svg'],
      manifest: {
        id: '/',
        name: 'snp',
        short_name: 'snp',
        description: 'Personal snippet manager, served from your tailnet',
        start_url: '/',
        scope: '/',
        display: 'standalone',
        background_color: '#0f172a',
        theme_color: '#0f172a',
        icons: [
          { src: '/pwa-192x192.png', sizes: '192x192', type: 'image/png', purpose: 'any' },
          { src: '/pwa-512x512.png', sizes: '512x512', type: 'image/png', purpose: 'any' },
          {
            src: '/pwa-192x192-maskable.png',
            sizes: '192x192',
            type: 'image/png',
            purpose: 'maskable',
          },
          {
            src: '/pwa-512x512-maskable.png',
            sizes: '512x512',
            type: 'image/png',
            purpose: 'maskable',
          },
        ],
      },
      workbox: {
        // SPA: deep links fall back to the precached index.html (the Go
        // server does the same for requests that reach it).
        navigateFallback: '/index.html',
        runtimeCaching: [
          // Never cache API responses (spec §6): reads come from the
          // local cache, and writes must always hit the server.
          {
            urlPattern: /^\/api\/.*/i,
            handler: 'NetworkOnly',
          },
        ],
      },
    }),
  ],
  server: {
    // Dev: proxy /api to the snp dev server (default :8080; override with
    // VITE_API_PROXY when the dev server runs elsewhere).
    proxy: {
      '/api': {
        target: process.env.VITE_API_PROXY ?? 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  resolve: {
    // Vitest resolves node_modules with node-ish conditions by default, which
    // makes `svelte` resolve to its server entry (no `mount`). Force the
    // browser condition so component tests get the client runtime.
    conditions: ['browser'],
  },
  test: {
    environment: 'jsdom',
    include: ['src/**/*.test.ts'],
    passWithNoTests: true,
  },
})
