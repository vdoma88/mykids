import type { FastifyInstance } from 'fastify';
import { z } from 'zod';
import { policySchema, storeItemSchema, timeWindowSchema } from '@mykids/contracts';
import { underPrefix, type Services } from '../app.js';
import { HttpError } from '../app.js';

/** Проверяет, что ребёнок принадлежит семье вошедшего родителя. */
async function assertOwnChild(s: Services, familyId: string, childId: string): Promise<void> {
  const child = await s.prisma.child.findUnique({ where: { id: childId }, select: { familyId: true } });
  if (!child || child.familyId !== familyId) {
    throw new HttpError(404, 'not_found', 'Ребёнок не найден.');
  }
}

function assertCanEdit(role: string): void {
  if (role === 'viewer') throw new HttpError(403, 'forbidden', 'Только для чтения.');
}

/** Что агент не считает и не закрывает никогда: рабочий стол и сам агент. */
const DEFAULT_ALWAYS_ALLOWED = ['explorer.exe', 'mykids-agent.exe'];

const policyBody = z.object({
  timezone: z.string().min(1),
  dailyLimitMinutes: z.array(z.number().int().nonnegative()).length(7),
  carryOverMaxMinutes: z.number().int().nonnegative(),
  windows: z.array(timeWindowSchema),
  // Необязательно намеренно: админка его не редактирует и не присылает. С
  // default([]) каждое сохранение правил стирало список, который сервер сам
  // завёл при создании ребёнка, — и Проводник переставал быть «всегда
  // разрешён» после первой же правки лимитов.
  alwaysAllowed: z.array(z.string()).optional(),
  economy: z.object({
    creditsPerMinute: z.number().int().positive(),
    maxConvertedMinutesPerDay: z.number().int().nonnegative(),
    minCreditsToConvert: z.number().int().nonnegative(),
    maxCreditsPerDay: z.number().int().positive(),
  }),
});

export function registerAdminRoutes(app: FastifyInstance, s: Services): void {
  app.addHook('onRequest', async (req) => {
    if (underPrefix(req.url, '/admin')) await app.requireGuardian(req);
  });

  app.get('/admin/children', async (req) => {
    const children = await s.prisma.child.findMany({
      where: { familyId: req.guardian!.familyId },
      include: { devices: { where: { revokedAt: null } }, policy: true },
      orderBy: { createdAt: 'asc' },
    });
    return Promise.all(children.map(async (c) => ({
      id: c.id, name: c.name, birthYear: c.birthYear,
      balances: await s.ledger.balances(c.id),
      devices: c.devices.map((d) => ({
        id: d.id, platform: d.platform, name: d.name,
        lastSeenAt: d.lastSeenAt, agentVersion: d.agentVersion,
      })),
      hasPolicy: c.policy !== null,
    })));
  });

  /**
   * Один ребёнок. Странице ребёнка нужно его имя: без него заголовок гласил
   * просто «Ребёнок», и с двумя детьми было не понять, чьи правила правишь.
   */
  app.get('/admin/children/:childId', async (req) => {
    const { childId } = z.object({ childId: z.string().uuid() }).parse(req.params);
    await assertOwnChild(s, req.guardian!.familyId, childId);
    const c = await s.prisma.child.findUniqueOrThrow({
      where: { id: childId },
      include: { devices: { where: { revokedAt: null }, orderBy: { createdAt: 'asc' } } },
    });
    return {
      id: c.id, name: c.name, birthYear: c.birthYear,
      balances: await s.ledger.balances(c.id),
      devices: c.devices.map((d) => ({
        id: d.id, platform: d.platform, name: d.name,
        lastSeenAt: d.lastSeenAt, agentVersion: d.agentVersion,
      })),
    };
  });

  app.post('/admin/children', async (req, reply) => {
    assertCanEdit(req.guardian!.role);
    const body = z.object({
      name: z.string().min(1).max(60),
      birthYear: z.number().int().min(2000).max(2030).optional(),
    }).parse(req.body);

    const child = await s.prisma.child.create({
      data: { familyId: req.guardian!.familyId, name: body.name, birthYear: body.birthYear ?? null },
    });
    // Ребёнок без политики бесполезен: агент не знает, что разрешать.
    // Белый список не пуст — ребёнок должен иметь возможность позвонить родителю.
    await s.prisma.policy.create({
      data: {
        childId: child.id,
        dailyLimitMinutes: [120, 60, 60, 60, 60, 90, 120],
        alwaysAllowed: DEFAULT_ALWAYS_ALLOWED,
      },
    });
    return reply.status(201).send({ id: child.id });
  });

  /**
   * Пакеты заданий ребёнка: что назначено и что вообще есть.
   *
   * Каталог сервер читает там же, откуда отдаёт содержимое, а не хранит
   * отдельным списком в базе: два списка разъехались бы, и родитель назначал
   * бы пакет, которого нет.
   */
  app.get('/admin/children/:childId/packs', async (req) => {
    const { childId } = z.object({ childId: z.string().uuid() }).parse(req.params);
    await assertOwnChild(s, req.guardian!.familyId, childId);

    const assigned = await s.prisma.packAssignment.findMany({
      where: { childId, enabled: true },
      select: { packId: true },
    });
    return { assigned: assigned.map((a) => a.packId), catalog: s.catalog() };
  });

  /** Полностью задаёт набор: что не прислали — снимается. */
  app.put('/admin/children/:childId/packs', async (req) => {
    assertCanEdit(req.guardian!.role);
    const { childId } = z.object({ childId: z.string().uuid() }).parse(req.params);
    await assertOwnChild(s, req.guardian!.familyId, childId);
    const { packs } = z.object({ packs: z.array(z.string().min(1)).max(200) }).parse(req.body);

    // Назначить можно только то, что есть в каталоге: иначе ребёнок увидит
    // пакет, который не загрузится, и решит, что сломалось у него.
    const known = new Set(s.catalog().map((p) => p.id));
    const unknown = packs.filter((id) => !known.has(id));
    if (unknown.length > 0) {
      throw new HttpError(400, 'unknown_pack', `Нет таких пакетов: ${unknown.join(', ')}`);
    }

    // Снятые пакеты удаляем, а не помечаем: попытки и начисления живут в
    // журнале отдельно, и терять с назначением нечего.
    await s.prisma.$transaction([
      s.prisma.packAssignment.deleteMany({ where: { childId, packId: { notIn: packs } } }),
      ...packs.map((packId) => s.prisma.packAssignment.upsert({
        where: { childId_packId: { childId, packId } },
        create: { childId, packId, enabled: true },
        update: { enabled: true },
      })),
    ]);
    return { assigned: packs };
  });

  app.get('/admin/children/:childId/policy', async (req) => {
    const { childId } = z.object({ childId: z.string().uuid() }).parse(req.params);
    await assertOwnChild(s, req.guardian!.familyId, childId);
    return s.economy.policyFor(childId);
  });

  app.put('/admin/children/:childId/policy', async (req) => {
    assertCanEdit(req.guardian!.role);
    const { childId } = z.object({ childId: z.string().uuid() }).parse(req.params);
    await assertOwnChild(s, req.guardian!.familyId, childId);
    const body = policyBody.parse(req.body);
    // Ещё раз прогоняем через доменную схему: она источник истины о форме политики.
    // alwaysAllowed в неё не входит и отбрасывается — схема strict и упала бы
    // на лишнем ключе.
    const { alwaysAllowed: _allowlist, ...domainShape } = body;
    policySchema.parse(domainShape);

    await s.prisma.policy.upsert({
      where: { childId },
      create: {
        childId, timezone: body.timezone,
        dailyLimitMinutes: body.dailyLimitMinutes,
        carryOverMaxMinutes: body.carryOverMaxMinutes,
        windows: body.windows as never,
        alwaysAllowed: body.alwaysAllowed ?? DEFAULT_ALWAYS_ALLOWED,
        ...body.economy,
      },
      update: {
        timezone: body.timezone,
        dailyLimitMinutes: body.dailyLimitMinutes,
        carryOverMaxMinutes: body.carryOverMaxMinutes,
        windows: body.windows as never,
        ...(body.alwaysAllowed === undefined ? {} : { alwaysAllowed: body.alwaysAllowed }),
        ...body.economy,
      },
    });
    return s.economy.policyFor(childId);
  });

  app.get('/admin/children/:childId/ledger', async (req) => {
    const { childId } = z.object({ childId: z.string().uuid() }).parse(req.params);
    await assertOwnChild(s, req.guardian!.familyId, childId);
    const { limit } = z.object({ limit: z.coerce.number().int().min(1).max(500).default(100) })
      .parse(req.query);

    const rows = await s.prisma.ledgerEntry.findMany({
      where: { childId }, orderBy: { recordedAt: 'desc' }, take: limit,
    });
    return { balances: await s.ledger.balances(childId), entries: rows };
  });

  /**
   * События вмешательства. Родитель должен их видеть: записывать подкрутку
   * часов и не показывать её — то же, что не записывать.
   */
  app.get('/admin/children/:childId/tampers', async (req) => {
    const { childId } = z.object({ childId: z.string().uuid() }).parse(req.params);
    await assertOwnChild(s, req.guardian!.familyId, childId);
    const { limit, unreviewed } = z.object({
      limit: z.coerce.number().int().min(1).max(200).default(50),
      unreviewed: z.coerce.boolean().default(false),
    }).parse(req.query);

    const [events, pending] = await Promise.all([
      s.prisma.tamperEvent.findMany({
        where: { childId, ...(unreviewed ? { reviewedAt: null } : {}) },
        orderBy: { recordedAt: 'desc' },
        take: limit,
        include: { device: { select: { name: true } } },
      }),
      s.prisma.tamperEvent.count({ where: { childId, reviewedAt: null } }),
    ]);
    return { pending, events };
  });

  /** Отметить события разобранными. Штраф, если нужен, идёт отдельной правкой. */
  app.post('/admin/children/:childId/tampers/review', async (req) => {
    assertCanEdit(req.guardian!.role);
    const { childId } = z.object({ childId: z.string().uuid() }).parse(req.params);
    await assertOwnChild(s, req.guardian!.familyId, childId);
    const body = z.object({ ids: z.array(z.string().uuid()).min(1).max(200) }).parse(req.body);

    // childId в условии обязателен: без него родитель мог бы закрыть событие
    // чужого ребёнка, подставив его идентификатор.
    const { count } = await s.prisma.tamperEvent.updateMany({
      where: { childId, id: { in: body.ids }, reviewedAt: null },
      data: { reviewedAt: new Date() },
    });
    return { reviewed: count, pending: await s.prisma.tamperEvent.count({ where: { childId, reviewedAt: null } }) };
  });

  /** Ручная корректировка. Комментарий обязателен — журнал должен объяснять себя. */
  app.post('/admin/children/:childId/adjust', async (req, reply) => {
    assertCanEdit(req.guardian!.role);
    const { childId } = z.object({ childId: z.string().uuid() }).parse(req.params);
    await assertOwnChild(s, req.guardian!.familyId, childId);
    const body = z.object({
      currency: z.enum(['minutes', 'credits']),
      amount: z.number().int().refine((v) => v !== 0, 'Ноль не имеет смысла.'),
      note: z.string().min(3).max(300),
    }).parse(req.body);

    await s.prisma.ledgerEntry.create({
      data: {
        childId, currency: body.currency, amount: body.amount,
        reason: 'manual_adjust', refType: 'guardian', refId: req.guardian!.id,
        deviceId: null, occurredAt: new Date(), seq: 0, note: body.note,
      },
    });
    return reply.status(201).send({ balances: await s.ledger.balances(childId) });
  });

  app.post('/admin/children/:childId/devices', async (req, reply) => {
    assertCanEdit(req.guardian!.role);
    const { childId } = z.object({ childId: z.string().uuid() }).parse(req.params);
    await assertOwnChild(s, req.guardian!.familyId, childId);
    const body = z.object({
      platform: z.enum(['windows', 'android', 'web']),
      name: z.string().min(1).max(60),
    }).parse(req.body);

    // Токен возвращается один раз: в базе остаётся только его хеш.
    return reply.status(201).send(await s.auth.enrollDevice({ childId, ...body }));
  });

  app.delete('/admin/devices/:deviceId', async (req, reply) => {
    assertCanEdit(req.guardian!.role);
    const { deviceId } = z.object({ deviceId: z.string().uuid() }).parse(req.params);
    const device = await s.prisma.device.findUnique({
      where: { id: deviceId }, include: { child: { select: { familyId: true } } },
    });
    if (!device || device.child.familyId !== req.guardian!.familyId) {
      throw new HttpError(404, 'not_found', 'Устройство не найдено.');
    }
    await s.auth.revokeDevice(deviceId);
    return reply.status(204).send();
  });

  app.get('/admin/store', async (req) => {
    return s.prisma.storeItem.findMany({
      where: { familyId: req.guardian!.familyId }, orderBy: { createdAt: 'asc' },
    });
  });

  app.post('/admin/store', async (req, reply) => {
    assertCanEdit(req.guardian!.role);
    const body = storeItemSchema.omit({ id: true }).parse(req.body);
    const created = await s.prisma.storeItem.create({
      data: {
        familyId: req.guardian!.familyId,
        title: body.title, description: body.description ?? null,
        costCurrency: body.cost.currency, costAmount: body.cost.amount,
        effect: body.effect as never,
        maxPerDay: body.maxPerDay ?? null, maxPerWeek: body.maxPerWeek ?? null,
        cooldownHours: body.cooldownHours ?? null,
        requiresApproval: body.requiresApproval, enabled: body.enabled,
      },
    });
    return reply.status(201).send({ id: created.id });
  });

  /**
   * Включить или выключить товар.
   *
   * Удалять нельзя: на товар ссылаются прошлые покупки, и журнал должен их
   * объяснять. Выключенный товар пропадает из магазина ребёнка, но остаётся в
   * истории. До этого маршрута ошибочно заведённый товар висел у ребёнка
   * навсегда.
   */
  app.patch('/admin/store/:id', async (req) => {
    assertCanEdit(req.guardian!.role);
    const { id } = z.object({ id: z.string().uuid() }).parse(req.params);
    const body = z.object({ enabled: z.boolean() }).parse(req.body);
    const updated = await s.prisma.storeItem.updateMany({
      where: { id, familyId: req.guardian!.familyId },
      data: { enabled: body.enabled },
    });
    if (updated.count === 0) throw new HttpError(404, 'not_found', 'Товар не найден.');
    return { id, enabled: body.enabled };
  });

  /** Очередь покупок и заданий, ждущих подтверждения родителя. */
  app.get('/admin/approvals', async (req) => {
    const familyId = req.guardian!.familyId;
    const [purchases, attempts] = await Promise.all([
      s.prisma.purchase.findMany({
        where: { status: 'pending', child: { familyId } },
        include: { storeItem: true, child: { select: { id: true, name: true } } },
        orderBy: { createdAt: 'asc' },
      }),
      s.prisma.attempt.findMany({
        where: { status: 'pending_approval', child: { familyId } },
        include: { child: { select: { id: true, name: true } } },
        orderBy: { createdAt: 'asc' },
      }),
    ]);
    // Родителю показываем текст задания и название пакета, а не
    // «psy-w01-practice-1»: подтверждать надо то, что он может узнать.
    const titles = new Map(s.catalog().map((p) => [p.id, p.title]));
    return {
      purchases,
      attempts: attempts.map((a) => {
        let stem: string | null = null;
        try { stem = s.content.terms(a.packId, a.itemId).item.stem; } catch { /* пакет убрали */ }
        return { ...a, stem, packTitle: titles.get(a.packId) ?? null };
      }),
    };
  });

  /**
   * Родитель подтверждает задание, которое машиной не проверить: «прибрался в
   * комнате», «позвонил бабушке». Кредиты назначаются в этот момент, по цене
   * из пакета и сегодняшним потолкам, — подтверждение снимает только то
   * условие, которое сервер проверить не мог, а не правила экономики.
   */
  app.post('/admin/attempts/:id/approve', async (req) => {
    assertCanEdit(req.guardian!.role);
    const { id } = z.object({ id: z.string().uuid() }).parse(req.params);
    return s.economy.approveAttempt(id, req.guardian!.familyId, new Date());
  });

  app.post('/admin/attempts/:id/reject', async (req, reply) => {
    assertCanEdit(req.guardian!.role);
    const { id } = z.object({ id: z.string().uuid() }).parse(req.params);
    await s.economy.rejectAttempt(id, req.guardian!.familyId, new Date());
    return reply.status(204).send();
  });

  /** Одобрить покупку: цена уже списана, выдаётся эффект. */
  app.post('/admin/purchases/:id/approve', async (req, reply) => {
    assertCanEdit(req.guardian!.role);
    const { id } = z.object({ id: z.string().uuid() }).parse(req.params);
    await s.economy.approvePurchase(id, req.guardian!.familyId, new Date());
    return reply.status(204).send();
  });

  /** Отклонить покупку: цена возвращается ребёнку отдельной строкой журнала. */
  app.post('/admin/purchases/:id/reject', async (req, reply) => {
    assertCanEdit(req.guardian!.role);
    const { id } = z.object({ id: z.string().uuid() }).parse(req.params);
    await s.economy.rejectPurchase(id, req.guardian!.familyId, new Date());
    return reply.status(204).send();
  });
}
