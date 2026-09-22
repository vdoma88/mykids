import type { FastifyInstance } from 'fastify';
import { z } from 'zod';
import { earnedCredits, grade } from '@mykids/task-runner/grade';
import { answerSchema } from '../answer.js';
import { HttpError, pathOf, type Services } from '../app.js';

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
   * Ответ на задание. Присылается сам ответ — и больше ничего.
   *
   * Раньше страница присылала ещё и свою оценку, цену задания, потолок пакета
   * и cooldown, а сервер их применял. То есть ребёнок называл себе цену сам:
   * весь антифарм обходился одним запросом мимо страницы. Пакеты сервер
   * отдаёт со своего диска — там же лежат и правильный ответ, и цена, и
   * потолок, и всё это время он мог посмотреть их сам.
   *
   * Осталось ровно одно, что по-прежнему принимается на слово: результат
   * виджета у типа `interactive`, потому что считает его браузер.
   */
  app.post('/child/attempts', async (req, reply) => {
    const body = z.object({
      packId: z.string().min(1),
      itemId: z.string().min(1),
      answer: answerSchema,
    }).parse(req.body);
    const childId = req.device!.childId;

    // Пакет должен быть назначен именно этому ребёнку и именно сейчас.
    // Без этой проверки кредиты приносил бы любой пакет из каталога, включая
    // те, что родитель снял.
    const assigned = await s.prisma.packAssignment.findFirst({
      where: { childId, packId: body.packId, enabled: true },
      select: { id: true },
    });
    if (!assigned) {
      throw new HttpError(403, 'pack_not_assigned', `Пакет «${body.packId}» тебе не назначен.`);
    }

    const terms = s.content.terms(body.packId, body.itemId);
    if (terms.item.type !== body.answer.type) {
      throw new HttpError(
        400, 'answer_type_mismatch',
        `Ответ типа «${body.answer.type}» не подходит заданию типа «${terms.item.type}».`,
      );
    }

    const verdict = grade(terms.item, body.answer);
    const result = await s.economy.awardTask({
      childId,
      packId: body.packId,
      itemId: body.itemId,
      score: verdict.score,
      baseCredits: earnedCredits(terms.item, verdict, terms.creditsPerCorrect),
      packDailyCreditCap: terms.dailyCreditCap,
      cooldownHours: terms.cooldownHours,
      answer: body.answer,
      pendingApproval: verdict.pendingApproval === true,
      at: new Date(),
    });

    // Оценку возвращаем: считает её теперь сервер, и страница ребёнка должна
    // показывать его ответ, а не свой. Две оценки на одно задание — это две
    // версии правды, и расходиться они начнут молча.
    return reply.status(201).send({
      ...result,
      score: verdict.score,
      ...(verdict.feedback === undefined ? {} : { feedback: verdict.feedback }),
      balances: await s.ledger.balances(childId),
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
