#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TARGET="$ROOT/third_party/lostcity"
URL="https://github.com/LostCityRS/Server.git"

if [[ -d "$TARGET/.git" ]]; then
  echo "Lost City checkout already exists: $TARGET"
  echo "Review upstream changes before updating it manually."
  exit 0
fi

if [[ -e "$TARGET" ]]; then
  echo "Target exists but is not a git checkout: $TARGET" >&2
  exit 1
fi

mkdir -p "$(dirname "$TARGET")"
echo "Cloning LostCityRS/Server into $TARGET"
git clone "$URL" "$TARGET"
echo "Bootstrap complete. Read third_party/lostcity/README.md and the upstream README before starting the server."
