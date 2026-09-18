#!/usr/bin/env bash
# Проверка агента против настоящего API.
#
#   agents/windows/integration.sh
#
# Поднимает сервер на временной базе, заводит семью с ребёнком и устройством
# через тот же REST, которым пользуется админка, и прогоняет go-тесты с меткой
# integration. Фальшивый сервер в юнит-тестах отвечает тем, что написано рядом
# с ними, и потому не поймает расхождения схем — этот прогон ловит.
set -euo pipefail

cd "$(dirname "$0")"
ROOT=$(cd ../.. && pwd)
API_DIR="$ROOT/server/api"
PORT=${MYKIDS_TEST_PORT:-3198}
DB=${DATABASE_URL_E2E:-postgresql://postgres@127.0.0.1:5433/mykids_e2e}
BASE="http://127.0.0.1:$PORT"

# Чужой процесс на нашем порту сделал бы результат бессмысленным.
if curl -fsS --max-time 1 "$BASE/health" >/dev/null 2>&1; then
  echo "порт $PORT уже занят: остановите старый сервер" >&2
  exit 1
fi

# setsid: обычный kill снимает обёртку npx, а сервер остаётся жить и держит
# порт — следующий прогон тихо ходил бы в устаревший процесс.
setsid env DATABASE_URL="$DB" PORT="$PORT" npx tsx "$API_DIR/src/main.ts" \
  >/tmp/mykids-api-integration.log 2>&1 &
API_PGID=$!
cleanup() { kill -- "-$API_PGID" 2>/dev/null || true; }
trap cleanup EXIT

for _ in $(seq 1 80); do
  curl -fsS --max-time 1 "$BASE/health" >/dev/null 2>&1 && break
  sleep 0.2
done
if ! curl -fsS --max-time 1 "$BASE/health" >/dev/null 2>&1; then
  echo "сервер не поднялся, лог:" >&2
  tail -20 /tmp/mykids-api-integration.log >&2
  exit 1
fi

json() { python3 -c "import sys,json;print(json.load(sys.stdin)$1)"; }

EMAIL="agent-$(date +%s)@example.com"
TOKEN=$(curl -fsS -X POST "$BASE/auth/register" -H 'content-type: application/json' \
  -d "{\"familyName\":\"Проверка агента\",\"email\":\"$EMAIL\",\"password\":\"очень-длинный-пароль\"}" \
  | json "['token']")

CHILD=$(curl -fsS -X POST "$BASE/admin/children" -H 'content-type: application/json' \
  -H "authorization: Bearer $TOKEN" -d '{"name":"Марк"}' | json "['id']")

DEVICE_TOKEN=$(curl -fsS -X POST "$BASE/admin/children/$CHILD/devices" \
  -H 'content-type: application/json' -H "authorization: Bearer $TOKEN" \
  -d '{"platform":"windows","name":"ПК"}' | json "['token']")

echo "сервер: $BASE, ребёнок: $CHILD"

MYKIDS_SERVER="$BASE" MYKIDS_DEVICE_TOKEN="$DEVICE_TOKEN" \
  go test -tags integration -count=1 -v ./test/ "$@"
