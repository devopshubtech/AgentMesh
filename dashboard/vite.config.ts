import { fileURLToPath, URL } from 'node:url';
import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  server: {
    port: 13000,
    strictPort: true,
    proxy: {
      '/v1': {
        target: 'http://localhost:18080',
        changeOrigin: true,
        configure: (proxy) => {
          // Server-Sent Events: the proxy pipes the response through as it arrives; make sure
          // nothing downstream tries to buffer or transform (compress) the stream.
          proxy.on('proxyRes', (proxyRes) => {
            const ct = proxyRes.headers['content-type'] ?? '';
            if (ct.includes('text/event-stream')) {
              proxyRes.headers['cache-control'] = 'no-cache, no-transform';
              proxyRes.headers['x-accel-buffering'] = 'no';
              delete proxyRes.headers['content-length'];
            }
          });
        },
      },
    },
  },
  preview: {
    port: 13000,
    strictPort: true,
  },
  build: {
    // Keep everything as external files so the strict CSP (no inline) is satisfied.
    assetsInlineLimit: 0,
    sourcemap: false,
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.ts'],
  },
});
