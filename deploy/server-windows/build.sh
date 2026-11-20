#!/usr/bin/env bash
# Установщик домашнего сервера MyKids для Windows: MyKids-Server-Setup-X.Y.Z.exe.
#
#   VERSION=0.9.1 deploy/server-windows/build.sh
#
# Собирается на любой ОС с Go, Node, unzip и makensis. Внутри — Node.js,
# PostgreSQL, собранный API и служба mykids-server, которая всем этим
# управляет. Docker на машине семьи не нужен.
set -euo pipefail
cd "$(dirname "$0")/../.."
ROOT=$(pwd)

VERSION=${VERSION:-}
case "$VERSION" in
  [0-9]*.[0-9]*.[0-9]*) ;;
  *) echo "задайте VERSION из трёх чисел, например VERSION=0.9.1 $0" >&2; exit 1 ;;
esac
command -v makensis >/dev/null || { echo "нужен makensis: apt-get install nsis" >&2; exit 1; }

# Версии и суммы закреплены: установщик семьи не должен зависеть от того,
# что лежит на чужом сервере в день сборки. Обновляя версию, обновите сумму.
NODE_VERSION=22.23.5
NODE_SHA256=4a191082e9706866830379d577d2cc8a50c776fcd9436a8d061b063ed0d9a0e3
PG_VERSION=16.15-1
PG_SHA256=25e6fcdfb8caec38691bf461125e7564508760666f7b8e5dc6a5f0818f58f81e

CACHE=${CACHE:-$ROOT/build/cache}
STAGE=$ROOT/build/server-win
DIST=$ROOT/agents/windows/dist
mkdir -p "$CACHE" "$DIST"

fetch() { # url файл сумма
  if [ ! -f "$CACHE/$2" ] || ! echo "$3  $CACHE/$2" | sha256sum -c --quiet - 2>/dev/null; then
    echo "скачиваю $2…"
    curl -fsSL -o "$CACHE/$2.part" "$1"
    mv "$CACHE/$2.part" "$CACHE/$2"
  fi
  echo "$3  $CACHE/$2" | sha256sum -c --quiet - || { echo "сумма $2 не сошлась — файл подменён или испорчен" >&2; rm -f "$CACHE/$2"; exit 1; }
}
fetch "https://nodejs.org/dist/v$NODE_VERSION/node-v$NODE_VERSION-win-x64.zip" node.zip "$NODE_SHA256"
fetch "https://get.enterprisedb.com/postgresql/postgresql-$PG_VERSION-windows-x64-binaries.zip" pgsql.zip "$PG_SHA256"

rm -rf "$STAGE"
mkdir -p "$STAGE/node" "$STAGE/licenses"

echo "Node.js $NODE_VERSION…"
unzip -q -j -o "$CACHE/node.zip" "node-v$NODE_VERSION-win-x64/node.exe" -d "$STAGE/node"
unzip -q -p "$CACHE/node.zip" "node-v$NODE_VERSION-win-x64/LICENSE" > "$STAGE/licenses/node.txt"

echo "PostgreSQL $PG_VERSION…"
# Из трёхсот мегабайт архива серверу нужна треть: программы, их библиотеки
# и общие файлы. pgAdmin, StackBuilder, заголовки и документация — нет.
unzip -q -o "$CACHE/pgsql.zip" 'pgsql/bin/*' 'pgsql/lib/*' 'pgsql/share/*' -d "$STAGE"
unzip -q -p "$CACHE/pgsql.zip" pgsql/server_license.txt > "$STAGE/licenses/postgresql.txt"
(cd "$STAGE/pgsql" && rm -rf bin/wx*.dll bin/stackbuilder.exe bin/*.pdb lib/*.lib lib/pgxs share/doc)

echo "API…"
TARGET=win32 OUT="$STAGE/app" deploy/server-windows/bundle.sh

echo "служба и окно управления…"
(cd agents/windows && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath \
  -ldflags "-s -w -X main.version=$VERSION -H windowsgui" -o "$STAGE/mykids-server.exe" ./cmd/mykids-server)
cp deploy/installer/mykids.ico "$STAGE/mykids.ico"

echo "установщик…"
OUT="$DIST/MyKids-Server-Setup-$VERSION.exe"
makensis -V2 -WX -INPUTCHARSET UTF8 -DVERSION="$VERSION" -DSTAGE="$STAGE" -DOUTFILE="$OUT" \
  deploy/server-windows/server.nsi
echo "собрано: $(du -h "$OUT" | cut -f1)  $OUT"
