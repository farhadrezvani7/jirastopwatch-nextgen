#!/usr/bin/env bash
# One-line install for Linux, no Go needed:
#   curl -fsSL https://raw.githubusercontent.com/farhadrezvani7/jirastopwatch-nextgen/main/install.sh | bash
# Inside a clone of the repo it builds from source instead (needs Go).
set -euo pipefail
REPO=farhadrezvani7/jirastopwatch-nextgen

# Build from source only when this script is a real file inside a clone (not when piped from curl).
src=${BASH_SOURCE[0]:-}
if [ -n "$src" ] && [ -f "$src" ] && [ -f "$(dirname "$src")/go.mod" ] && command -v go >/dev/null; then
  cd "$(dirname "$src")"
  go build -trimpath -ldflags "-s -w" -o jirastopwatch-nextgen .
  bin=./jirastopwatch-nextgen
else
  [ "$(uname -s)" = Linux ] || { echo "This script is for Linux. Use the installer from the Releases page."; exit 1; }
  case "$(uname -m)" in x86_64|amd64) arch=amd64;; aarch64|arm64) arch=arm64;; *) echo "Unsupported CPU: $(uname -m)"; exit 1;; esac
  tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
  bin=$tmp/jirastopwatch-nextgen
  curl -fsSL "https://github.com/$REPO/releases/latest/download/jirastopwatch-nextgen-linux-$arch" -o "$bin"
  chmod +x "$bin"
fi
"$bin" --install
