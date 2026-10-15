#!/usr/bin/env bash
# Cross-compiles release binaries into dist/ (no Windows/macOS machine needed).
set -euo pipefail
cd "$(dirname "$0")"
# VERSION=1.0.0 UPDATE_REPO=owner/name ./build-all.sh  -> enables the "update available" notice
VERSION=${VERSION:-dev}; UPDATE_REPO=${UPDATE_REPO:-}
go test ./...
for t in linux/amd64 linux/arm64 windows/amd64 windows/arm64 darwin/amd64 darwin/arm64; do
  os=${t%/*}; arch=${t#*/}; out=dist/jirastopwatch-nextgen-$os-$arch; flags="-s -w -X main.version=$VERSION -X main.updateRepo=$UPDATE_REPO"
  # windowsgui: no console window behind the app on Windows
  [ "$os" = windows ] && out+=.exe && flags+=" -H=windowsgui"
  GOOS=$os GOARCH=$arch go build -trimpath -ldflags "$flags" -o "$out" .
  echo "built $out"
done
(cd dist && { sha256sum jirastopwatch-nextgen-* 2>/dev/null || shasum -a 256 jirastopwatch-nextgen-*; } > SHA256SUMS.txt)
echo "wrote dist/SHA256SUMS.txt"
