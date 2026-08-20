#!/bin/sh
# 交叉編譯 glow 到所有平台，輸出到 dist/
# 用法: sh scripts/release.sh
set -e
cd "$(dirname "$0")/.."

OUT=dist
mkdir -p "$OUT"

for target in linux-amd64 linux-arm64 darwin-amd64 darwin-arm64; do
  GOOS=${target%-*}
  GOARCH=${target#*-}
  echo "==> building glow-$target"
  GOOS=$GOOS GOARCH=$GOARCH CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w" \
    -o "$OUT/glow-$target" .
done

# 目前平台（例如 windows-amd64）
HOST=$(go env GOOS)-$(go env GOARCH)
echo "==> building glow-$HOST"
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$OUT/glow-$HOST" .

echo ""
echo "Done:"
ls -lh "$OUT"
