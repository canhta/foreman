import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';

const backendPort = process.env.FOREMAN_DASHBOARD_PORT || '8080';

export default defineConfig({
  plugins: [svelte(), tailwindcss()],
  build: {
    outDir: '../dist',
    emptyOutDir: true,
  },
  server: {
    proxy: {
      '/api': `http://127.0.0.1:${backendPort}`,
      '/ws': { target: `ws://127.0.0.1:${backendPort}`, ws: true },
    },
  },
});
