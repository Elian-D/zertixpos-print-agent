#!/usr/bin/env bash
# Compila el agente. Desde WSL o Linux, con Go 1.25 o más nuevo:
#
#   scripts/build.sh 0.1.0
#
# Deja en dist/:
#   zertixpos-agent.exe     el de Windows (el que se publica y se instala en el negocio)
#   zertixpos-agent-linux   para desarrollar y probar por IP desde WSL
#   SHA256SUMS              las sumas de los dos
set -euo pipefail

VERSION="${1:-}"
if [[ -z "$VERSION" ]]; then
    echo "Uso: scripts/build.sh <versión>   (por ejemplo 0.1.0, sin la v)" >&2
    exit 1
fi
VERSION="${VERSION#v}"

cd "$(dirname "${BASH_SOURCE[0]}")/.."
export PATH="$PATH:/usr/local/go/bin"
# Que no baje otra versión de Go por su cuenta: compila con la instalada.
export GOTOOLCHAIN=local

LDFLAGS="-s -w -X main.version=${VERSION}"

echo "Pruebas…"
go vet ./...
GOOS=windows GOARCH=amd64 go vet ./...
go test ./...

mkdir -p dist
rm -f dist/zertixpos-agent.exe dist/zertixpos-agent-linux dist/SHA256SUMS

echo "Windows (amd64)…"
# -H windowsgui: sin consola negra para el servicio y la bandeja (los comandos piden una si la necesitan).
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "${LDFLAGS} -H windowsgui" -o dist/zertixpos-agent.exe .

echo "Linux (amd64)…"
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "${LDFLAGS}" -o dist/zertixpos-agent-linux .

(cd dist && sha256sum zertixpos-agent.exe zertixpos-agent-linux > SHA256SUMS)

echo
ls -lh dist
echo
echo "Listo: dist/zertixpos-agent.exe (versión ${VERSION})"
