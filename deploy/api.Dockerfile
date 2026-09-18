# Сборка API. Контекст — корень репозитория: нужны общие пакеты монорепы.
FROM node:22-slim AS base
WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends openssl \
    && rm -rf /var/lib/apt/lists/*

COPY package.json package-lock.json tsconfig.base.json ./
COPY packages/contracts/package.json packages/contracts/
COPY packages/domain/package.json packages/domain/
COPY packages/task-runner/package.json packages/task-runner/
COPY packages/content-tools/package.json packages/content-tools/
COPY server/api/package.json server/api/
RUN npm ci --omit=dev --ignore-scripts

COPY packages/ packages/
COPY server/api/ server/api/

# Клиент Prisma генерируется под платформу образа, а не хостовую
RUN npx prisma generate --schema server/api/prisma/schema.prisma
RUN npm install --no-save tsx

ENV NODE_ENV=production
EXPOSE 3000

# Миграции применяются на старте: для домашнего развёртывания это проще,
# чем отдельный шаг, а migrate deploy идемпотентна.
CMD ["sh", "-c", "npx prisma migrate deploy --schema server/api/prisma/schema.prisma && npx tsx server/api/src/main.ts"]
