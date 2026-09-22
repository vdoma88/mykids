#!/usr/bin/env sh
# Сборка MSI из уже собранных .exe. Запускается на любой ОС: wixl из msitools
# делает MSI без Windows.
#
#   agents/windows/build.sh      # сначала .exe
#   VERSION=0.7.0 deploy/build-msi.sh
set -eu
cd "$(dirname "$0")/.."

DIST=${DIST:-agents/windows/dist}
OUT=${OUT:-$DIST/mykids.msi}

if ! command -v wixl >/dev/null 2>&1; then
  echo "нужен wixl: apt-get install wixl (пакет msitools)" >&2
  exit 1
fi

# Версия обязательна и обязана быть числовой: Windows сравнивает версии
# пакетов по числам, и «0.7.0-dev» не разберёт. Значение по умолчанию было бы
# хуже отсутствия — пакет врал бы о себе, и обновление поверх не сработало бы.
VERSION=${VERSION:-}
if [ -z "$VERSION" ]; then
  echo "задайте VERSION, например VERSION=0.7.0 $0" >&2
  exit 1
fi
case "$VERSION" in
  *[!0-9.]*|''|.*|*.) echo "версия должна быть числовой: 0.7.0, а не «$VERSION»" >&2; exit 1 ;;
esac

for f in mykids-agent.exe mykids-setup.exe mykids-check.exe; do
  [ -f "$DIST/$f" ] || { echo "нет $DIST/$f — сначала agents/windows/build.sh" >&2; exit 1; }
done

wixl -o "$OUT" -D Version="$VERSION" -D Dist="$DIST" --arch x64 deploy/mykids.wxs
echo "собрано: $(du -h "$OUT" | cut -f1)  $OUT"
