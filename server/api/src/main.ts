import { buildApp } from './app.js';
import { closeDb, db } from './db.js';

const port = Number(process.env['PORT'] ?? 3000);
const app = buildApp(db());

const shutdown = async (): Promise<void> => {
  await app.close();
  await closeDb();
  process.exit(0);
};
process.on('SIGINT', () => void shutdown());
process.on('SIGTERM', () => void shutdown());

app.listen({ port, host: '0.0.0.0' })
  .then(() => console.log(`MyKids API слушает порт ${port}`))
  .catch((err: unknown) => {
    console.error('не удалось запустить сервер:', err);
    process.exit(1);
  });
