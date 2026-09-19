import { fileURLToPath } from 'node:url';
import { buildApp } from './app.js';
import { closeDb, db } from './db.js';

const port = Number(process.env['PORT'] ?? 3000);

// Собранный интерфейс лежит рядом с API — и в репозитории, и в образе.
// Путь считаем от себя, а не от текущего каталога: сервер запускают из
// разных мест, и «работает, только если запустить из корня» — это ошибка,
// которая находится в самый неподходящий момент.
const webRoot = process.env['MYKIDS_WEB_ROOT']
  ?? fileURLToPath(new URL('../../admin-ui/dist', import.meta.url));

// Пакеты заданий лежат в корне репозитория и попадают в образ рядом.
const contentRoot = process.env['MYKIDS_CONTENT_ROOT']
  ?? fileURLToPath(new URL('../../../content/packs', import.meta.url));

const app = buildApp(db(), { webRoot, contentRoot });

const shutdown = async (): Promise<void> => {
  await app.close();
  await closeDb();
  process.exit(0);
};
process.on('SIGINT', () => void shutdown());
process.on('SIGTERM', () => void shutdown());

app.listen({ port, host: '0.0.0.0' })
  .then(() => console.log(`MyKids слушает порт ${port}\n  интерфейс: ${webRoot}\n  задания:   ${contentRoot}`))
  .catch((err: unknown) => {
    console.error('не удалось запустить сервер:', err);
    process.exit(1);
  });
