#!/usr/bin/env bash
# Builds JiraStopWatch NextGen and adds a launcher (Linux app grid / macOS ~/Applications).
set -euo pipefail
cd "$(dirname "$0")"
go build -trimpath -ldflags "-s -w" -o jirastopwatch-nextgen .
if [ "$(uname -s)" = Darwin ]; then
  echo "Built ./jirastopwatch-nextgen — run it from Terminal, or move it somewhere on your PATH."
  exit 0
fi
mkdir -p ~/.local/share/applications
cat > ~/.local/share/applications/jirastopwatch-nextgen.desktop <<DESK
[Desktop Entry]
Type=Application
Name=Jira StopWatch NextGen
Comment=Track time on Jira issues
Exec=$PWD/jirastopwatch-nextgen
Icon=appointment-soon
Categories=Office;ProjectManagement;
StartupWMClass=jirastopwatch-nextgen
DESK
echo "Installed. Search for 'Jira StopWatch NextGen' in your applications menu."
