# Образ сервера MyKids: API и интерфейс одним адресом.
#
# Контекст сборки — корень репозитория: нужны общие пакеты монорепы.
#
#   docker build -f deploy/api.Dockerfile -t mykids .

# --- Сборка интерфейса -------------------------------------------------------
#
# Отдельной ступенью, потому что vite и typescript нужны только здесь.
# Тащить их в рабочий образ незачем: это лишние сотни мегабайт и лишняя
# поверхность в том, что стоит дома у семьи.
FROM node:22-slim AS web
WORKDIR /app

COPY package.json package-lock.json tsconfig.base.json ./
COPY packages/contracts/package.json packages/contracts/
COPY packages/domain/package.json packages/domain/
COPY packages/task-runner/package.json packages/task-runner/
COPY packages/content-tools/package.json packages/content-tools/
COPY server/api/package.json server/api/
COPY server/admin-ui/package.json server/admin-ui/
RUN npm ci --ignore-scripts

COPY packages/ packages/
COPY server/admin-ui/ server/admin-ui/
RUN npm run build --workspace @mykids/admin-ui

# --- Рабочий образ -----------------------------------------------------------
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
COPY server/admin-ui/package.json server/admin-ui/
RUN npm ci --omit=dev --ignore-scripts

COPY packages/ packages/
COPY server/api/ server/api/
COPY --from=web /app/server/admin-ui/dist server/admin-ui/dist
# Пакеты заданий: без них ребёнку нечем зарабатывать кредиты, и вся
# экономика держится на ручных начислениях родителя.
COPY content/packs/ content/packs/

# Prisma CLI и tsx — той же версии, что в package-lock, и внутри образа.
# Раньше их не было: npx скачивал prisma при каждом запуске контейнера, и
# сервер без интернета не стартовал вовсе, а с интернетом получал последнюю
# версию — какую угодно, в том числе несовместимую со схемой.
RUN npm install --no-save --ignore-scripts \
      "prisma@$(node -p "require('@prisma/client/package.json').version")" \
      "tsx@$(node -p "require('./package-lock.json').packages['node_modules/tsx'].version")"

# Клиент Prisma генерируется под платформу образа, а не хостовую
RUN npx --no-install prisma generate --schema server/api/prisma/schema.prisma

ENV NODE_ENV=production
EXPOSE 3000

LABEL org.opencontainers.image.source="https://github.com/vdoma88/mykids" \
      org.opencontainers.image.title="MyKids" \
      org.opencontainers.image.description="Домашний сервер MyKids: админка родителя, задания ребёнка, учёт экранного времени"

# Здоровье — для графических интерфейсов NAS: «работает» там должно значить
# «отвечает», а не «процесс не упал». curl в образе нет, а node есть.
HEALTHCHECK --interval=30s --timeout=5s --start-period=60s --retries=3 \
  CMD ["node", "-e", "fetch('http://127.0.0.1:3000/health').then(r=>process.exit(r.ok?0:1),()=>process.exit(1))"]

# Миграции применяются на старте: для домашнего развёртывания это проще,
# чем отдельный шаг, а migrate deploy идемпотентна. --no-install: ничего не
# скачивать на старте, всё уже в образе.
CMD ["sh", "-c", "npx --no-install prisma migrate deploy --schema server/api/prisma/schema.prisma && npx --no-install tsx server/api/src/main.ts"]
