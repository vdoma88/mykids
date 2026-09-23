import { defineConfig, type ProxyOptions } from 'vite';
import react from '@vitejs/plugin-react';

const pkg = (rel: string): string => new URL(`../../packages/${rel}`, import.meta.url).pathname;

/**
 * Прокси на API, который пропускает мимо себя переходы по страницам.
 *
 * Браузер, открывающий страницу, просит text/html, а запросы клиента API —
 * JSON. Страницы — дело vite и роутера, иначе обновление /children/42 в
 * разработке уходило бы на сервер и возвращало 404.
 */
function api(): ProxyOptions {
  return {
    target: 'http://127.0.0.1:3000',
    bypass: (req) => (req.headers.accept?.includes('text/html') ? '/index.html' : undefined),
  };
}

export default defineConfig({
  plugins: [react()],
  resolve: {
    // Регулярки с якорями, а не строки: строковый псевдоним заменяет префикс,
    // и «@mykids/task-runner/runner.css» превращался в «…/index.ts/runner.css».
    alias: [
      { find: /^@mykids\/contracts$/, replacement: pkg('contracts/src/index.ts') },
      { find: /^@mykids\/domain$/, replacement: pkg('domain/src/index.ts') },
      { find: /^@mykids\/task-runner$/, replacement: pkg('task-runner/src/index.ts') },
      { find: /^@mykids\/task-runner\/runner\.css$/, replacement: pkg('task-runner/src/ui/runner.css') },
    ],
  },
  server: {
    port: 5174,
    // В разработке фронт и API живут на разных портах; проксирование убирает
    // возню с CORS и делает пути в коде такими же, как в продакшене.
    proxy: {
      '/auth': 'http://127.0.0.1:3000',
      '/admin': api(),
      // «/child» — это и страница ребёнка, и её запросы к серверу. Страницу
      // отдаёт vite, запросы уходят на API: иначе в разработке открыть
      // /child было нельзя вовсе — прокси отправлял на API и саму страницу.
      '/child': api(),
      '/agent': 'http://127.0.0.1:3000',
      '/health': 'http://127.0.0.1:3000',
      // Пакеты заданий: без этого ребёнку в разработке нечего решать.
      '/content': 'http://127.0.0.1:3000',
    },
  },
  build: { outDir: 'dist', emptyOutDir: true },
});
