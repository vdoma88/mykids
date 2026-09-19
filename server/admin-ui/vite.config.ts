import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@mykids/contracts': new URL('../../packages/contracts/src/index.ts', import.meta.url).pathname,
      '@mykids/domain': new URL('../../packages/domain/src/index.ts', import.meta.url).pathname,
      '@mykids/task-runner': new URL('../../packages/task-runner/src/index.ts', import.meta.url).pathname,
    },
  },
  server: {
    port: 5174,
    // В разработке фронт и API живут на разных портах; проксирование убирает
    // возню с CORS и делает пути в коде такими же, как в продакшене.
    proxy: {
      '/auth': 'http://127.0.0.1:3000',
      '/admin': 'http://127.0.0.1:3000',
      '/child': 'http://127.0.0.1:3000',
      '/agent': 'http://127.0.0.1:3000',
      '/health': 'http://127.0.0.1:3000',
    },
  },
  build: { outDir: 'dist', emptyOutDir: true },
});
