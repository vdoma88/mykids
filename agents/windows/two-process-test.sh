#!/usr/bin/env bash
# Проверка разделения службы и помощника на двух настоящих процессах.
#
#   agents/windows/two-process-test.sh
#
# Юнит-тесты проверяют протокол и учёт по отдельности. Здесь проверяется то,
# что видно только на живом запуске: что время действительно списывается с той
# скоростью, с какой идёт, и что снятие помощника не останавливает счёт.
#
# Именно так нашлась потеря времени на округлении: помощник шлёт наблюдения
# каждую секунду, таймер службы тикает своим чередом, промежутки выходят по
# полсекунды — и час экрана превращался в считанные минуты.
set -euo pipefail

cd "$(dirname "$0")"
BIN=$(mktemp -d)/mykids-agent
DATA=$(mktemp -d)
SOCK="$DATA/agent.sock"
LIMIT_SECONDS=${LIMIT_SECONDS:-60}

go build -o "$BIN" ./cmd/mykids-agent

# Минута в день: лимит кончится за время прогона.
cat > "$DATA/policy.json" <<EOF
{"timezone":"UTC","dailyLimitMinutes":[1,1,1,1,1,1,1],"carryOverMaxMinutes":0,
 "windows":[],"alwaysAllowed":["explorer.exe"],"idleThresholdSeconds":120,"warnBeforeMinutes":1}
EOF

pids=()
cleanup() {
  for p in "${pids[@]:-}"; do kill "$p" 2>/dev/null || true; done
  wait 2>/dev/null || true
}
trap cleanup EXIT

"$BIN" -data "$DATA" -pipe "$SOCK" -interval 1s serve > "$DATA/serve.log" 2>&1 &
pids+=($!)

for _ in $(seq 1 50); do [ -S "$SOCK" ] && break; sleep 0.2; done
if [ ! -S "$SOCK" ]; then
  echo "служба не открыла канал:" >&2; cat "$DATA/serve.log" >&2; exit 1
fi

MYKIDS_FAKE_PROCESS=game.exe "$BIN" -data "$DATA" -pipe "$SOCK" -interval 1s helper \
  > "$DATA/helper.log" 2>&1 &
helper_pid=$!
pids+=("$helper_pid")

echo "служба и помощник запущены, лимит ${LIMIT_SECONDS} с"

# Ждём, пока лимит кончится и помощник закроет экран. С запасом вдвое: если
# время списывается медленнее, чем идёт, мы это и хотим поймать.
deadline=$(( $(date +%s) + LIMIT_SECONDS * 2 + 15 ))
until grep -q "ЭКРАН ЗАКРЫТ" "$DATA/helper.log" 2>/dev/null; do
  if [ "$(date +%s)" -gt "$deadline" ]; then
    echo "за ${LIMIT_SECONDS}-секундный лимит экран так и не закрылся —" >&2
    echo "время списывается медленнее, чем идёт" >&2
    tail -20 "$DATA/serve.log" "$DATA/helper.log" >&2
    exit 1
  fi
  sleep 1
done
echo "лимит исчерпан, помощник закрыл экран"

if ! grep -q "закончилось" "$DATA/helper.log"; then
  echo "ребёнку не объяснили причину:" >&2; cat "$DATA/helper.log" >&2; exit 1
fi

# Снимаем помощника: служба обязана продолжать считать, а не считать это отдыхом.
kill "$helper_pid" 2>/dev/null || true
before=$(python3 -c "import json;print(json.load(open('$DATA/state.json'))['today']['usedSeconds'])")
sleep 40  # дольше порога молчания и дольше периода сохранения
after=$(python3 -c "import json;print(json.load(open('$DATA/state.json'))['today']['usedSeconds'])")

if [ "$after" -le "$before" ]; then
  echo "после снятия помощника счёт остановился: было $before, стало $after" >&2
  echo "снять наблюдателя не должно быть способом получить бесплатное время" >&2
  exit 1
fi
echo "помощник снят, служба продолжает считать: $before -> $after"
echo "разделение службы и помощника: все проверки пройдены"
