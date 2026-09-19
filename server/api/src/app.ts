import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import Fastify, { type FastifyInstance, type FastifyReply, type FastifyRequest } from 'fastify';
import fastifyStatic from '@fastify/static';
import type { PrismaClient, Guardian, Device } from '@prisma/client';
import { AuthError, AuthService } from './services/auth.service.js';
import { EconomyService, NotFoundError, RuleError } from './services/economy.service.js';
import { LedgerService } from './services/ledger.service.js';
import { registerAuthRoutes } from './routes/auth.routes.js';
import { registerAdminRoutes } from './routes/admin.routes.js';
import { registerChildRoutes } from './routes/child.routes.js';
import { registerAgentRoutes } from './routes/agent.routes.js';

/** Настройки сборки приложения. */
export interface AppOptions {
  /**
   * Каталог собранного интерфейса. Если задан и существует, сервер отдаёт
   * его с того же адреса, что и API.
   *
   * Одним адресом, а не двумя: семья разворачивает это дома, и «откройте
   * админку на 5174, а API живёт на 3000» — это ровно та инструкция, на
   * которой домашнее развёртывание и заканчивается. Заодно отпадает CORS
   * и становится настоящим адрес заданий, который агент пишет ребёнку на
   * закрытом экране.
   */
  webRoot?: string | undefined;
  /**
   * Каталог с пакетами заданий. Если задан и существует, сервер отдаёт их
   * ребёнку — иначе зарабатывать кредиты ему нечем.
   *
   * Без проверки токена: правильные ответы всё равно попадают в браузер,
   * потому что проверяет ответы он сам. Приватная часть — не содержание
   * пакетов, а то, какие из них назначены этому ребёнку, и она остаётся
   * за «/child/packs».
   */
  contentRoot?: string | undefined;
}

/** Пакет в каталоге — то, что видно родителю при назначении. */
export interface PackSummary {
  id: string;
  title: string;
  subject: string;
  version: string;
  itemCount: number;
  description?: string;
}

export interface Services {
  prisma: PrismaClient;
  auth: AuthService;
  economy: EconomyService;
  ledger: LedgerService;
  /**
   * Каталог пакетов заданий: читается оттуда же, откуда отдаётся содержимое.
   * Отдельным списком в базе он разъехался бы с каталогом на диске, и
   * родитель назначал бы ребёнку пакет, которого нет.
   */
  catalog: () => PackSummary[];
}

declare module 'fastify' {
  interface FastifyRequest {
    guardian?: Guardian;
    device?: Device;
  }
}

export class HttpError extends Error {
  constructor(readonly status: number, readonly code: string, message: string) {
    super(message);
  }
}

/**
 * Читает каталог пакетов с диска при каждом обращении.
 *
 * Без кэша намеренно: пакеты добавляют редко, а перезапускать сервер ради
 * нового пакета — та мелочь, из-за которой контент перестают добавлять вовсе.
 */
function readCatalog(contentRoot: string | undefined): PackSummary[] {
  if (!contentRoot) return [];
  const index = join(contentRoot, 'index.json');
  if (!existsSync(index)) return [];
  try {
    const raw = JSON.parse(readFileSync(index, 'utf8')) as { packs?: PackSummary[] };
    return raw.packs ?? [];
  } catch {
    // Битый индекс не должен ронять весь сервер: без каталога родитель не
    // назначит пакеты, но всё остальное работает.
    return [];
  }
}

/** Путь без строки запроса. */
export function pathOf(url: string): string {
  return url.split('?')[0] ?? '';
}

/**
 * Лежит ли путь под префиксом — сам префикс или что-то глубже него.
 *
 * Именно с границей, а не просто startsWith: «/children» начинается с
 * «/child», но это страница родителя, а не запрос ребёнка. Проверка без
 * границы отдавала родителю 401 на его собственном списке детей, и нашлось
 * это только тогда, когда сервер начал отдавать страницы.
 */
export function underPrefix(url: string, prefix: string): boolean {
  const path = pathOf(url);
  return path === prefix || path.startsWith(prefix + '/');
}

/**
 * Запрос к API, а не к странице.
 *
 * «/child» — особый случай, и обойти его нечем: сама страница ребёнка и
 * есть «/child», а всё, что глубже, — её запросы к серверу. Развести их
 * можно только по границе пути.
 */
export function isApiRequest(url: string): boolean {
  if (pathOf(url).startsWith('/child/')) return true;
  return ['/auth', '/admin', '/agent', '/health', '/content'].some((p) => underPrefix(url, p));
}

/** Достаёт токен из заголовка Authorization. */
function bearer(req: FastifyRequest): string | null {
  const header = req.headers.authorization;
  if (!header?.startsWith('Bearer ')) return null;
  return header.slice('Bearer '.length).trim() || null;
}

export function buildApp(prisma: PrismaClient, options: AppOptions = {}): FastifyInstance {
  const services: Services = {
    prisma,
    auth: new AuthService(prisma),
    economy: new EconomyService(prisma),
    ledger: new LedgerService(prisma),
    catalog: () => readCatalog(options.contentRoot),
  };

  const app = Fastify({ logger: false });

  /** Требует сессию родителя. */
  app.decorate('requireGuardian', async (req: FastifyRequest) => {
    const token = bearer(req);
    const guardian = token ? await services.auth.guardianByToken(token) : null;
    if (!guardian) throw new HttpError(401, 'unauthorized', 'Нужен вход в аккаунт родителя.');
    req.guardian = guardian;
  });

  /** Требует токен устройства. */
  app.decorate('requireDevice', async (req: FastifyRequest) => {
    const token = bearer(req);
    const device = token ? await services.auth.deviceByToken(token) : null;
    if (!device) throw new HttpError(401, 'unauthorized', 'Неизвестный токен устройства.');
    req.device = device;
  });

  // Ошибки домена и аутентификации отдаются кодами, а не текстом: клиенту
  // нужно отличать «мало кредитов» от «дневной лимит исчерпан».
  app.setErrorHandler((err, _req, reply) => {
    if (err instanceof HttpError) {
      return reply.status(err.status).send({ error: err.code, message: err.message });
    }
    if (err instanceof AuthError) {
      const status = err.code === 'invalid_credentials' ? 401 : 400;
      return reply.status(status).send({ error: err.code, message: err.message });
    }
    if (err instanceof RuleError) {
      return reply.status(409).send({ error: err.code, message: err.message });
    }
    if (err instanceof NotFoundError) {
      return reply.status(404).send({ error: 'not_found', message: err.message });
    }
    // zod и встроенная валидация Fastify приходят разными формами
    const asRecord = err as { validation?: unknown; statusCode?: number; name?: string; message?: string };
    if (asRecord.validation || asRecord.statusCode === 400 || asRecord.name === 'ZodError') {
      return reply.status(400).send({ error: 'bad_request', message: asRecord.message ?? 'Некорректный запрос.' });
    }
    app.log.error(err);
    return reply.status(500).send({ error: 'internal', message: 'Внутренняя ошибка сервера.' });
  });

  app.get('/health', async () => ({ ok: true }));

  registerAuthRoutes(app, services);
  registerAdminRoutes(app, services);
  registerChildRoutes(app, services);
  registerAgentRoutes(app, services);

  registerContent(app, options.contentRoot);
  registerWeb(app, options.webRoot);

  return app;
}

/** Просит ли браузер страницу, а не JSON. */
function wantsPage(req: FastifyRequest): boolean {
  if (req.method !== 'GET' && req.method !== 'HEAD') return false;
  const accept = req.headers.accept ?? '';
  return accept.includes('text/html');
}

/**
 * Отдаёт пакеты заданий.
 *
 * Читает их раннер в браузере ребёнка тем же загрузчиком, что и страница
 * локального прогона: pack.json и файлы заданий по одному. Поэтому здесь
 * обычная раздача каталога, а не свой формат — иначе загрузчиков стало бы
 * два, и разошлись бы они молча.
 */
function registerContent(app: FastifyInstance, contentRoot: string | undefined): void {
  if (!contentRoot || !existsSync(join(contentRoot, 'index.json'))) {
    return;
  }
  void app.register(fastifyStatic, {
    root: contentRoot,
    prefix: '/content/packs/',
    // Раздача уже одна зарегистрирована для страниц; повторно украшать
    // reply.sendFile плагин не даст.
    decorateReply: false,
  });
}

/**
 * Отдаёт собранный интерфейс с того же адреса, что и API.
 *
 * Роутинг у React свой, поэтому неизвестный путь — это не обязательно ошибка:
 * это может быть ссылка вроде /children/42, которую сервер видит впервые.
 * Такие отдаём index.html. Но только их: неизвестный путь под /admin/ или
 * /agent/ — настоящая ошибка, и отвечать на неё страницей значило бы
 * прятать опечатку в адресе от того, кто её сделал.
 */
function registerWeb(app: FastifyInstance, webRoot: string | undefined): void {
  if (!webRoot || !existsSync(join(webRoot, 'index.html'))) {
    return;
  }

  void app.register(fastifyStatic, { root: webRoot, wildcard: false });

  app.setNotFoundHandler((req: FastifyRequest, reply: FastifyReply) => {
    if (!wantsPage(req) || isApiRequest(req.url)) {
      return reply.status(404).send({ error: 'not_found', message: 'Нет такого пути.' });
    }
    return reply.sendFile('index.html');
  });
}

declare module 'fastify' {
  interface FastifyInstance {
    requireGuardian(req: FastifyRequest): Promise<void>;
    requireDevice(req: FastifyRequest): Promise<void>;
  }
}
