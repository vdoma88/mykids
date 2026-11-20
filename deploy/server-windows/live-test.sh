#!/usr/bin/env bash
# Живая проверка сервера без Docker: собирает комплект под эту машину и
# гоняет супервизор на настоящих PostgreSQL и Node — с пустого каталога до
# регистрации семьи, остановки и повторного запуска.
#
#   deploy/server-windows/live-test.sh
#
# Нужны PostgreSQL 16 (initdb, pg_ctl, psql, pg_dump) и Node 22. Каталог
# программ PostgreSQL ищется сам; задать явно — MYKIDS_PG_BIN.
# От root PostgreSQL не запускается — запускайте от обычного пользователя.
set -euo pipefail
cd "$(dirname "$0")/../.."
ROOT=$(pwd)

PG_BIN=${MYKIDS_PG_BIN:-}
if [ -z "$PG_BIN" ]; then
  for d in /usr/lib/postgresql/*/bin; do [ -x "$d/initdb" ] && PG_BIN=$d; done
fi
[ -n "$PG_BIN" ] || { echo "не нашёл initdb; задайте MYKIDS_PG_BIN" >&2; exit 1; }

HOME_DIR=${MYKIDS_SERVER_HOME:-$ROOT/build/server-native}
if [ "${SKIP_BUNDLE:-}" != 1 ]; then
  TARGET=native OUT="$HOME_DIR/app" deploy/server-windows/bundle.sh
fi

cd agents/windows
MYKIDS_SERVER_HOME="$HOME_DIR" MYKIDS_PG_BIN="$PG_BIN" MYKIDS_NODE="${MYKIDS_NODE:-$(command -v node)}" \
  go test -tags live -count=1 -v -run TestSupervisor ./internal/serverctl/ "$@"
