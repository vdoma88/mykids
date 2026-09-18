import { PrismaClient } from '@prisma/client';

export type { PrismaClient };

let client: PrismaClient | undefined;

/** Единственный клиент на процесс: Prisma держит собственный пул соединений. */
export function db(): PrismaClient {
  client ??= new PrismaClient();
  return client;
}

export async function closeDb(): Promise<void> {
  await client?.$disconnect();
  client = undefined;
}
