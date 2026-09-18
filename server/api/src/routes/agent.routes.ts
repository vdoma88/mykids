import type { FastifyInstance } from 'fastify';
import { z } from 'zod';
import type { Services } from '../app.js';

/**
 * То, что нужно агенту на устройстве ребёнка.
 *
 * Асимметрия офлайна закреплена здесь: списание минут за экран агент присылает
 * постфактум и оно принимается, а начисление за задания проходит только через
 * /child/attempts, где сумму считает сервер.
 */
export function registerAgentRoutes(app: FastifyInstance, s: Services): void {
  app.addHook('onRequest', async (req) => {
    if (req.url.startsWith('/agent')) await app.requireDevice(req);
  });

  /** Политика и текущее состояние. Агент кэширует их и решает офлайн. */
  app.get('/agent/sync', async (req) => {
    const childId = req.device!.childId;
    await s.economy.ensureDailyGrant(childId, new Date());
    const [policy, balances, screen] = await Promise.all([
      s.economy.policyFor(childId),
      s.ledger.balances(childId),
      s.economy.screenState(childId, new Date()),
    ]);
    return { serverTime: new Date().toISOString(), policy, balances, screen };
  });

  /**
   * Пакет накопленного расхода экрана.
   *
   * Каждая запись несёт монотонный счётчик устройства: повторная отправка
   * очереди после обрыва связи не должна удвоить списание.
   */
  app.post('/agent/usage', async (req) => {
    const body = z.object({
      agentVersion: z.string().max(40).optional(),
      entries: z.array(z.object({
        seq: z.number().int().nonnegative(),
        minutes: z.number().int().positive().max(1440),
        occurredAt: z.string().datetime(),
      })).max(500),
    }).parse(req.body);

    const device = req.device!;
    if (body.agentVersion) {
      await s.prisma.device.update({
        where: { id: device.id }, data: { agentVersion: body.agentVersion },
      });
    }

    const accepted = await s.ledger.append(body.entries.map((e) => ({
      childId: device.childId,
      currency: 'minutes' as const,
      amount: -e.minutes,
      reason: 'screen_usage' as const,
      deviceId: device.id,
      occurredAt: new Date(e.occurredAt),
      seq: e.seq,
    })));

    return {
      accepted,
      duplicates: body.entries.length - accepted,
      balances: await s.ledger.balances(device.childId),
      screen: await s.economy.screenState(device.childId, new Date()),
    };
  });

  /** Событие вмешательства: остановка агента, отзыв разрешений. */
  app.post('/agent/tamper', async (req, reply) => {
    const body = z.object({
      kind: z.string().min(1).max(60),
      detail: z.string().max(300).optional(),
    }).parse(req.body);

    await s.prisma.ledgerEntry.create({
      data: {
        childId: req.device!.childId, currency: 'minutes', amount: 0,
        reason: 'tamper_penalty', refType: 'tamper', refId: body.kind,
        deviceId: req.device!.id, occurredAt: new Date(), seq: 0,
        note: body.detail ?? null,
      },
    });
    // Величину штрафа решает родитель через политику; пока только фиксируем факт.
    return reply.status(201).send({ recorded: true });
  });
}
