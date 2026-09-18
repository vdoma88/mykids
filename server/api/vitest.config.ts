import { defineConfig } from 'vitest/config';

/**
 * Тесты сервера вынесены из корневого конфига: им нужен настоящий Postgres,
 * а доменные тесты должны оставаться быстрыми и ни от чего не зависеть.
 */
export default defineConfig({
  test: {
    globals: true,
    include: ['test/**/*.test.ts'],
    // Транзакции и TRUNCATE между тестами не переживают параллельных файлов.
    fileParallelism: false,
    testTimeout: 20_000,
  },
  resolve: {
    alias: {
      '@mykids/contracts': new URL('../../packages/contracts/src/index.ts', import.meta.url).pathname,
      '@mykids/domain': new URL('../../packages/domain/src/index.ts', import.meta.url).pathname,
    },
  },
});
