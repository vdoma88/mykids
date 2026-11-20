#!/usr/bin/env sh
# Сборка установщика из уже собранных .exe. Запускается на любой ОС: makensis
# есть и под Linux (apt-get install nsis).
#
#   VERSION=0.9.0 agents/windows/build.sh
#   VERSION=0.9.0 deploy/build-installer.sh
set -eu
cd "$(dirname "$0")/.."

DIST=${DIST:-agents/windows/dist}

if ! command -v makensis >/dev/null 2>&1; then
  echo "нужен makensis: apt-get install nsis" >&2
  exit 1
fi

# Версия обязательна и обязана быть числовой: Windows сравнивает версии по
# числам, и по ней же новая версия решает, обновлять или отказаться.
# Значение по умолчанию было бы хуже отсутствия: установщик врал бы о себе.
VERSION=${VERSION:-}
case "$VERSION" in
  '') echo "задайте VERSION, например VERSION=0.9.0 $0" >&2; exit 1 ;;
  *[!0-9.]*|.*|*.|*..*) echo "версия должна быть числовой: 0.9.0, а не «$VERSION»" >&2; exit 1 ;;
esac
case "$VERSION" in
  *.*.*.*) echo "версия из трёх чисел: 0.9.0, а не «$VERSION»" >&2; exit 1 ;;
  *.*.*) ;;
  *) echo "версия из трёх чисел: 0.9.0, а не «$VERSION»" >&2; exit 1 ;;
esac

for f in mykids-agent.exe mykids-setup.exe mykids-check.exe; do
  [ -f "$DIST/$f" ] || { echo "нет $DIST/$f — сначала agents/windows/build.sh" >&2; exit 1; }
done

OUT=${OUT:-$DIST/MyKids-Setup-$VERSION.exe}
# Предупреждения — ошибки: молча проглоченное предупреждение NSIS — это,
# как правило, строка, которой в установщике не окажется.
makensis -V2 -WX -INPUTCHARSET UTF8 \
  -DVERSION="$VERSION" -DDIST="$(pwd)/$DIST" -DOUTFILE="$(pwd)/$OUT" \
  deploy/installer/mykids.nsi
echo "собрано: $(du -h "$OUT" | cut -f1)  $OUT"
