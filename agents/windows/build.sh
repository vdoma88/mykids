#!/usr/bin/env sh
# Сборка программ под Windows x64. Запускается на любой ОС.
set -eu
cd "$(dirname "$0")"
mkdir -p dist
# Версию подменяем только если её задали снаружи. Значение по умолчанию здесь
# было бы хуже, чем его отсутствие: забытая переменная превратила бы релиз в
# бинарник, который врёт о своей версии.
LDFLAGS="-s -w"
if [ -n "${VERSION:-}" ]; then
  LDFLAGS="$LDFLAGS -X main.version=$VERSION"
fi

CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath \
  -ldflags "$LDFLAGS" \
  -o dist/mykids-agent.exe ./cmd/mykids-agent

# Проверка собирается как оконная программа: «-H windowsgui» убирает чёрное
# окно консоли. Её запускают двойным щелчком, и мелькнувшая консоль — первое,
# что заставляет закрыть программу, не дочитав.
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath \
  -ldflags "$LDFLAGS -H windowsgui" \
  -o dist/mykids-check.exe ./cmd/mykids-check

# Установщик — тоже оконная программа, и по той же причине: его запускают
# двойным щелчком, чтобы не открывать командную строку. Мелькнувшая консоль
# свела бы весь смысл на нет.
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath \
  -ldflags "$LDFLAGS -H windowsgui" \
  -o dist/mykids-setup.exe ./cmd/mykids-setup

for f in dist/mykids-agent.exe dist/mykids-check.exe dist/mykids-setup.exe; do
  echo "собрано: $(du -h "$f" | cut -f1)  $f"
done
