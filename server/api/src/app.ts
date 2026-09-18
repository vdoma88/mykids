import Fastify, { type FastifyInstance, type FastifyRequest } from 'fastify';
import type { PrismaClient, Guardian, Device } from '@prisma/client';
import { AuthError, AuthService } from './services/auth.service.js';
import { EconomyService, NotFoundError, RuleError } from './services/economy.service.js';
import { LedgerService } from './services/ledger.service.js';
import { registerAuthRoutes } from './routes/auth.routes.js';
import { registerAdminRoutes } from './routes/admin.routes.js';
import { registerChildRoutes } from './routes/child.routes.js';
import { registerAgentRoutes } from './routes/agent.routes.js';

export interface Services {
  prisma: PrismaClient;
  auth: AuthService;
  economy: EconomyService;
  ledger: LedgerService;
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

/** Достаёт токен из заголовка Authorization. */
function bearer(req: FastifyRequest): string | null {
  const header = req.headers.authorization;
  if (!header?.startsWith('Bearer ')) return null;
  return header.slice('Bearer '.length).trim() || null;
}

export function buildApp(prisma: PrismaClient): FastifyInstance {
  const services: Services = {
    prisma,
    auth: new AuthService(prisma),
    economy: new EconomyService(prisma),
    ledger: new LedgerService(prisma),
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

  return app;
}

declare module 'fastify' {
  interface FastifyInstance {
    requireGuardian(req: FastifyRequest): Promise<void>;
    requireDevice(req: FastifyRequest): Promise<void>;
  }
}
