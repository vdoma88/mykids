import type { FastifyInstance } from 'fastify';
import { z } from 'zod';
import { pathOf, type Services } from '../app.js';

/**
 * То, что видит и делает ребёнок. Доступ по токену устройства: раннер живёт
 * на том же устройстве, что и агент.
 */
export function registerChildRoutes(app: FastifyInstance, s: Services): void {
  // Глубже «/child», а не «/child» целиком: сам «/child» — это страница
  // ребёнка, которую отдаёт сервер, и токен вводят уже на ней. Запросы со
  // страницы без токена устройства не отвечают.
  app.addHook('onRequest', async (req) => {
    if (pathOf(req.url).startsWith('/child/')) await app.requireDevice(req);
  });

  /** Баланс, правила и остаток — то же, что ребёнок видит у себя на экране. */
  app.get('/child/me', async (req) => {
    const childId = req.device!.childId;
    const [child, balances, policy, screen] = await Promise.all([
      s.prisma.child.findUniqueOrThrow({ where: { id: childId }, select: { name: true } }),
      s.ledger.balances(childId),
      s.economy.policyFor(childId),
      s.economy.screenState(childId, new Date()),
    ]);
    // Правила отдаются ребёнку намеренно: скрытая механика воспринимается
    // как несправедливость и провоцирует искать обход вместо игры по правилам.
    return { name: child.name, balances, policy, screen };
  });

  app.get('/child/packs', async (req) => {
    const assignments = await s.prisma.packAssignment.findMany({
      where: { childId: req.device!.childId, enabled: true },
      select: { packId: true },
    });
    return { packs: assignments.map((a) => a.packId) };
  });

  /**
   * Результат задания. Клиент присылает свою оценку, но сумму кредитов
   * назначает сервер: потолки и cooldown применяются здесь.
   */
  app.post('/child/attempts', async (req, reply) => {
    const body = z.object({
      packId: z.string().min(1),
      itemId: z.string().min(1),
      score: z.number().min(0).max(1),
      baseCredits: z.number().int().min(0).max(50),
      packDailyCreditCap: z.number().int().min(1).max(500),
      cooldownHours: z.number().nonnegative().optional(),
    }).parse(req.body);

    const result = await s.economy.awardTask({
      childId: req.device!.childId, ...body, at: new Date(),
    });
    return reply.status(201).send({
      ...result,
      balances: await s.ledger.balances(req.device!.childId),
    });
  });

  app.post('/child/convert', async (req) => {
    const body = z.object({ minutes: z.number().int().positive().max(600) }).parse(req.body);
    const result = await s.economy.convert(req.device!.childId, body.minutes, new Date());
    return { ...result, balances: await s.ledger.balances(req.device!.childId) };
  });

  app.get('/child/store', async (req) => {
    const child = await s.prisma.child.findUniqueOrThrow({
      where: { id: req.device!.childId }, select: { familyId: true },
    });
    return s.prisma.storeItem.findMany({
      where: { familyId: child.familyId, enabled: true }, orderBy: { costAmount: 'asc' },
    });
  });

  app.post('/child/purchases', async (req, reply) => {
    const body = z.object({ storeItemId: z.string().uuid() }).parse(req.body);
    const result = await s.economy.purchase(req.device!.childId, body.storeItemId, new Date());
    return reply.status(201).send({
      ...result, balances: await s.ledger.balances(req.device!.childId),
    });
  });
}
