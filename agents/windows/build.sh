#!/usr/bin/env sh
# Сборка агента под Windows x64. Запускается на любой ОС.
set -eu
cd "$(dirname "$0")"
mkdir -p dist
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath \
  -ldflags "-s -w -X main.version=${VERSION:-0.1.0}" \
  -o dist/mykids-agent.exe ./cmd/mykids-agent
echo "собрано: $(du -h dist/mykids-agent.exe | cut -f1)  dist/mykids-agent.exe"
