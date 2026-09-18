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
    const [policy, row, balances, screen] = await Promise.all([
      s.economy.policyFor(childId),
      s.prisma.policy.findUniqueOrThrow({ where: { childId }, select: { alwaysAllowed: true } }),
      s.ledger.balances(childId),
      s.economy.screenState(childId, new Date()),
    ]);
    return {
      serverTime: new Date().toISOString(),
      policy,
      // Белый список не входит в доменную схему политики, но нужен агенту:
      // без него ребёнок не сможет позвонить родителю при нулевом балансе.
      // Приходить он обязан с сервера, а не из локального файла, который
      // ребёнок может отредактировать.
      agent: { alwaysAllowed: row.alwaysAllowed },
      balances,
      screen,
    };
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

  /**
   * Событие вмешательства: перевод часов, остановка агента, отзыв разрешений.
   *
   * Пишется в отдельную таблицу, а не в журнал. Журнал хранит движение минут и
   * кредитов, а событие само по себе ничего не списывает: величину штрафа
   * решает родитель. К тому же журнал уникален по паре устройство+счётчик, и
   * второе событие с того же устройства в него бы не поместилось — а часы
   * ребёнок может переводить сколько угодно раз подряд.
   */
  app.post('/agent/tamper', async (req, reply) => {
    const body = z.object({
      kind: z.string().min(1).max(60),
      detail: z.string().max(300).optional(),
      // Время события по часам устройства. Необязательно: старый агент его не
      // шлёт, а отказывать ему значило бы потерять сообщение целиком.
      at: z.string().datetime().optional(),
    }).parse(req.body);

    const event = await s.prisma.tamperEvent.create({
      data: {
        childId: req.device!.childId,
        deviceId: req.device!.id,
        kind: body.kind,
        detail: body.detail ?? null,
        occurredAt: body.at ? new Date(body.at) : null,
      },
    });
    return reply.status(201).send({ recorded: true, id: event.id });
  });
}
