import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    globals: true,
    include: ['packages/*/test/**/*.test.ts'],
  },
  resolve: {
    alias: {
      '@mykids/contracts': new URL('./packages/contracts/src/index.ts', import.meta.url).pathname,
      '@mykids/domain': new URL('./packages/domain/src/index.ts', import.meta.url).pathname,
    },
  },
});
