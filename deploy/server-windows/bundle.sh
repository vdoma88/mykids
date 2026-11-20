#!/usr/bin/env bash
# Комплект сервера MyKids без Docker: API одним файлом, его зависимости,
# интерфейс, пакеты заданий и миграции.
#
#   TARGET=win32  deploy/server-windows/bundle.sh   # для установщика Windows
#   TARGET=native deploy/server-windows/bundle.sh   # для проверки на этой машине
#
# Результат — в $OUT (по умолчанию build/server-<target>/app).
#
# Node и PostgreSQL сюда не входят: их кладёт deploy/server-windows/build.sh,
# а для проверки на Linux берутся системные.
set -euo pipefail
cd "$(dirname "$0")/../.."
ROOT=$(pwd)
TARGET=${TARGET:-win32}
OUT=${OUT:-$ROOT/build/server-$TARGET/app}

rm -rf "$OUT"
mkdir -p "$OUT"

# Версии — из package-lock: комплект обязан совпадать с тем, что проверено
# тестами и что стоит в образе Docker.
ver() { node -p "require('./package-lock.json').packages['node_modules/$1'].version"; }
PRISMA=$(ver @prisma/client)
ARGON=$(ver @node-rs/argon2)

echo "интерфейс…"
npm run build -w @mykids/admin-ui >/dev/null
cp -r server/admin-ui/dist "$OUT/web"

echo "API одним файлом…"
# Нативные модули и Prisma — снаружи: у них свои файлы под платформу.
# createRequire — для зависимостей, написанных под CommonJS.
npx esbuild server/api/src/main.ts --bundle --platform=node --target=node22 --format=esm \
  --external:@prisma/client --external:@node-rs/argon2 --external:.prisma/client \
  --banner:js="import { createRequire as __mkCreateRequire } from 'node:module'; const require = __mkCreateRequire(import.meta.url);" \
  --legal-comments=none --log-level=warning \
  --outfile="$OUT/server.mjs"

echo "зависимости под $TARGET…"
cat > "$OUT/package.json" <<JSON
{
  "name": "mykids-server",
  "private": true,
  "type": "module",
  "dependencies": {
    "@prisma/client": "$PRISMA",
    "@node-rs/argon2": "$ARGON"
  },
  "devDependencies": {
    "prisma": "$PRISMA"
  }
}
JSON
platform=()
if [ "$TARGET" = win32 ]; then platform=(--os=win32 --cpu=x64); fi
(cd "$OUT" && npm install --no-audit --no-fund --ignore-scripts "${platform[@]}" >/dev/null)

echo "клиент Prisma под $TARGET…"
mkdir -p "$OUT/prisma"
engine=native
if [ "$TARGET" = win32 ]; then engine=windows; fi
# binaryTargets только здесь, а не в общей схеме: иначе образ Docker таскал
# бы движок Windows, а сборка на любой машине ходила бы за ним в сеть.
sed "s/provider = \"prisma-client-js\"/provider      = \"prisma-client-js\"\n  binaryTargets = [\"$engine\"]/" \
  server/api/prisma/schema.prisma > "$OUT/prisma/schema.prisma"
(cd "$OUT" && DATABASE_URL=postgresql://x@localhost/x ./node_modules/.bin/prisma generate --schema prisma/schema.prisma >/dev/null)
# Работающему серверу нужны три вещи: клиент Prisma, сгенерированный под
# платформу, и argon2 с его родным модулем. Prisma CLI с движками — ещё
# двести мегабайт — нужен был только для генерации; npm prune его не убирает,
# потому что клиент числит его необязательной зависимостью.
keep=$(mktemp -d)
mv "$OUT/node_modules/@prisma/client" "$keep/client"
mv "$OUT/node_modules/.prisma" "$keep/.prisma"
mv "$OUT/node_modules/@node-rs" "$keep/@node-rs"
rm -rf "$OUT/node_modules" "$OUT/prisma" "$OUT/package-lock.json"
mkdir -p "$OUT/node_modules/@prisma"
mv "$keep/client" "$OUT/node_modules/@prisma/client"
mv "$keep/.prisma" "$OUT/node_modules/.prisma"
mv "$keep/@node-rs" "$OUT/node_modules/@node-rs"
rmdir "$keep"
# Из рантайма клиента нужен только library.js — тот, что работает с родным
# движком. Остальное — движки WebAssembly под все базы на свете, сборки для
# edge и React Native, карты исходников: семьдесят мегабайт, которые родитель
# скачивал бы впустую.
(cd "$OUT/node_modules/@prisma/client/runtime" && rm -f ./*.map query_engine_bg.* query_compiler_bg.* \
  edge* react-native* wasm-* binary.* client.* index-browser.*)

cp -r server/api/prisma/migrations "$OUT/migrations"
rm -f "$OUT/migrations/migration_lock.toml"
cp -r content/packs "$OUT/packs"

du -sh "$OUT" | sed 's/^/комплект: /'
