#!/usr/bin/env bash
# Проверка разделения службы и помощника на двух настоящих процессах.
#
#   agents/windows/two-process-test.sh
#
# Юнит-тесты проверяют протокол и учёт по отдельности. Здесь проверяется то,
# что видно только на живом запуске: что время действительно списывается с той
# скоростью, с какой идёт, что предупреждение приходит раньше блокировки, что
# ребёнку объяснили причину, и что снятие помощника не останавливает счёт.
#
# Именно так нашлась потеря времени на округлении: помощник шлёт наблюдения
# каждую секунду, таймер службы тикает своим чередом, промежутки выходят по
# полсекунды — и час экрана превращался в считанные минуты.
set -euo pipefail

cd "$(dirname "$0")"
BIN=$(mktemp -d)/mykids-agent
DATA=$(mktemp -d)
SOCK="$DATA/agent.sock"
LIMIT_MINUTES=${LIMIT_MINUTES:-2}
LIMIT_SECONDS=$(( LIMIT_MINUTES * 60 ))

go build -o "$BIN" ./cmd/mykids-agent

# Две минуты в день с предупреждением за минуту: лимит кончится за время
# прогона, и первую минуту предупреждения быть не должно. Иначе проверка
# «предупредили заранее» прошла бы и для надписи, висящей всегда.
cat > "$DATA/policy.json" <<EOF
{"timezone":"UTC","dailyLimitMinutes":[$LIMIT_MINUTES,$LIMIT_MINUTES,$LIMIT_MINUTES,$LIMIT_MINUTES,$LIMIT_MINUTES,$LIMIT_MINUTES,$LIMIT_MINUTES],
 "carryOverMaxMinutes":0,
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

# Дальше — про то, что видит подросток. Проверять это на живом запуске нужно
# по той же причине, что и учёт: порядок событий во времени юнит-тестом не
# ловится, а именно он тут и важен.
line_of() { grep -n "$1" "$DATA/helper.log" | head -1 | cut -d: -f1; }
warned=$(line_of "ПРЕДУПРЕЖДЕНИЕ" || true)
blocked=$(line_of "ЭКРАН ЗАКРЫТ")

if [ -z "$warned" ]; then
  echo "экран закрылся без предупреждения:" >&2; cat "$DATA/helper.log" >&2
  echo "потерять экран посреди игры без предупреждения — худшее, что можно сделать" >&2
  exit 1
fi
if [ "$warned" -ge "$blocked" ]; then
  echo "предупреждение появилось не раньше блокировки:" >&2
  cat "$DATA/helper.log" >&2; exit 1
fi
echo "предупреждение пришло раньше блокировки (строки $warned и $blocked)"

if grep "ПРЕДУПРЕЖДЕНИЕ" "$DATA/helper.log" | grep -q "0 минут"; then
  echo "в предупреждении осталось «0 минут»:" >&2
  grep "ПРЕДУПРЕЖДЕНИЕ" "$DATA/helper.log" >&2
  echo "ноль — не предупреждение, а недоразумение: время ещё идёт" >&2
  exit 1
fi

# Закрытый экран без объяснения читается как наказание, а не как правило.
for expect in "кончилось" "Завтра будет"; do
  if ! grep -q "$expect" "$DATA/helper.log"; then
    echo "на закрытом экране нет «$expect»:" >&2; cat "$DATA/helper.log" >&2; exit 1
  fi
done
echo "на закрытом экране сказано, почему и что будет завтра"

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
