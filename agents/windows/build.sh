#!/usr/bin/env sh
# Сборка агента под Windows x64. Запускается на любой ОС.
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
echo "собрано: $(du -h dist/mykids-agent.exe | cut -f1)  dist/mykids-agent.exe"
