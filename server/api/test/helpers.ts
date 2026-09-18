import { PrismaClient } from '@prisma/client';

/**
 * Тесты идут против настоящего Postgres, а не мока: половина того, что здесь
 * проверяется, — поведение самой базы (уникальность пар устройство+счётчик,
 * транзакции, каскады). Мок этого не покажет.
 */
export const prisma = new PrismaClient({
  datasources: { db: { url: process.env['DATABASE_URL_TEST'] ?? process.env['DATABASE_URL'] ?? '' } },
});

export async function resetDb(): Promise<void> {
  // Порядок важен: снимаем зависимости до родителей.
  await prisma.$executeRawUnsafe(`
    TRUNCATE TABLE "LedgerEntry", "Attempt", "Purchase", "PackAssignment",
                   "Policy", "Device", "Session", "Child", "Guardian",
                   "StoreItem", "Family" RESTART IDENTITY CASCADE
  `);
}

export interface Fixture {
  familyId: string;
  childId: string;
  deviceId: string;
}

/** Семья с одним ребёнком и политикой по умолчанию. */
export async function seedFamily(over: Partial<{
  dailyLimitMinutes: number[];
  carryOverMaxMinutes: number;
  windows: unknown[];
  creditsPerMinute: number;
  maxConvertedMinutesPerDay: number;
  minCreditsToConvert: number;
  maxCreditsPerDay: number;
  timezone: string;
}> = {}): Promise<Fixture> {
  const family = await prisma.family.create({ data: { name: 'Тестовая семья' } });
  const child = await prisma.child.create({ data: { familyId: family.id, name: 'Марк' } });
  await prisma.policy.create({
    data: {
      childId: child.id,
      timezone: over.timezone ?? 'UTC',
      dailyLimitMinutes: over.dailyLimitMinutes ?? [60, 60, 60, 60, 60, 60, 60],
      carryOverMaxMinutes: over.carryOverMaxMinutes ?? 30,
      windows: (over.windows ?? []) as never,
      creditsPerMinute: over.creditsPerMinute ?? 2,
      maxConvertedMinutesPerDay: over.maxConvertedMinutesPerDay ?? 45,
      minCreditsToConvert: over.minCreditsToConvert ?? 10,
      maxCreditsPerDay: over.maxCreditsPerDay ?? 120,
    },
  });
  const device = await prisma.device.create({
    data: { childId: child.id, platform: 'windows', name: 'ПК', tokenHash: `t-${child.id}` },
  });
  return { familyId: family.id, childId: child.id, deviceId: device.id };
}

/** Прямая вставка в журнал, минуя сервисы, — для подготовки состояния. */
export async function giveCredits(childId: string, amount: number, at = new Date()): Promise<void> {
  await prisma.ledgerEntry.create({
    data: {
      childId, currency: 'credits', amount, reason: 'manual_adjust',
      deviceId: null, occurredAt: at, recordedAt: at, seq: 0,
    },
  });
}
