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

# --- installers: .deb/.rpm (Linux), setup .exe (Windows), .app zip (macOS) ---
# Needs nfpm, makensis and zip; any that is missing is skipped with a warning.
V=${VERSION#v}; [[ $V =~ ^[0-9] ]] || V=0.0.0
for arch in amd64 arm64; do
  if command -v nfpm >/dev/null; then
    for fmt in deb rpm; do
      sed "s|\${BIN}|dist/jirastopwatch-nextgen-linux-$arch|" packaging/nfpm.yaml > dist/nfpm.yaml
      ARCH=$arch VERSION=$V nfpm package -f dist/nfpm.yaml -p $fmt -t dist/jirastopwatch-nextgen-linux-$arch.$fmt >/dev/null
      echo "built dist/jirastopwatch-nextgen-linux-$arch.$fmt"
    done
  else echo "skip .deb/.rpm: nfpm not installed"; fi

  if command -v makensis >/dev/null; then
    makensis -V1 -DVERSION=$VERSION -DEXE="$PWD/dist/jirastopwatch-nextgen-windows-$arch.exe" \
      -DOUT="$PWD/dist/jirastopwatch-nextgen-windows-$arch-setup.exe" packaging/windows.nsi
    echo "built dist/jirastopwatch-nextgen-windows-$arch-setup.exe"
  else echo "skip Windows installer: makensis not installed"; fi

  if command -v zip >/dev/null; then
    app="dist/mac-$arch/Jira StopWatch NextGen.app"
    rm -rf "dist/mac-$arch"; mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
    cp dist/jirastopwatch-nextgen-darwin-$arch "$app/Contents/MacOS/jirastopwatch-nextgen"
    cp packaging/icon.icns "$app/Contents/Resources/icon.icns"
    sed "s/@VERSION@/$V/g" packaging/Info.plist > "$app/Contents/Info.plist"
    (cd "dist/mac-$arch" && zip -qry -X "../jirastopwatch-nextgen-macos-$arch.zip" "Jira StopWatch NextGen.app")
    rm -rf "dist/mac-$arch"
    echo "built dist/jirastopwatch-nextgen-macos-$arch.zip"
  else echo "skip macOS app: zip not installed"; fi
done

rm -f dist/nfpm.yaml
(cd dist && { sha256sum jirastopwatch-nextgen-* 2>/dev/null || shasum -a 256 jirastopwatch-nextgen-*; } > SHA256SUMS.txt)
echo "wrote dist/SHA256SUMS.txt"
