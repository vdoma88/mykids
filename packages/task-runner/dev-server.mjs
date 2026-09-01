#!/usr/bin/env node
/**
 * Статический сервер для локального прогона раннера.
 *
 *   node packages/task-runner/dev-server.mjs [порт]
 *
 * Отдаёт корень репозитория, чтобы раннер видел и свою сборку, и content/packs.
 */
import { createServer } from 'node:http';
import { createReadStream, statSync } from 'node:fs';
import { extname, join, normalize } from 'node:path';

const ROOT = new URL('../..', import.meta.url).pathname;
const PORT = Number(process.argv[2] ?? 5173);
const TYPES = {
  '.html': 'text/html; charset=utf-8', '.js': 'text/javascript; charset=utf-8',
  '.json': 'application/json; charset=utf-8', '.css': 'text/css; charset=utf-8',
  '.map': 'application/json; charset=utf-8',
};

createServer((req, res) => {
  const url = new URL(req.url ?? '/', 'http://localhost');
  const rel = decodeURIComponent(url.pathname);

  // Именно редирект, а не отдача файла: иначе относительный ./dist/app.js
  // из index.html разрешается в корень сервера и не находится.
  // Браузер всегда просит иконку; 404 на неё засоряет консоль и дымовой тест.
  if (rel === '/favicon.ico') {
    res.writeHead(204).end();
    return;
  }

  if (rel === '/') {
    res.writeHead(302, { location: '/packages/task-runner/index.html' }).end();
    return;
  }

  // normalize схлопывает ../, а префиксная проверка не выпускает за пределы корня
  const file = normalize(join(ROOT, rel));
  if (!file.startsWith(ROOT)) {
    res.writeHead(403).end('запрещено');
    return;
  }

  try {
    if (!statSync(file).isFile()) throw new Error('не файл');
  } catch {
    res.writeHead(404).end('не найдено');
    return;
  }

  res.writeHead(200, { 'content-type': TYPES[extname(file)] ?? 'application/octet-stream' });
  createReadStream(file).pipe(res);
}).listen(PORT, () => console.log(`раннер: http://localhost:${PORT}/`));
