#!/usr/bin/env bash
# AO3-Hub launcher: restarts the binary if it exits 0 (OTA update path).
# Place next to the binary. Run: ./launcher.sh
set -euo pipefail

BIN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN="${BIN_DIR}/ao3-hub"

if [ ! -x "$BIN" ]; then
  echo "ao3-hub binary not found at $BIN" >&2
  exit 1
fi

while true; do
  "$BIN" "$@"
  code=$?
  if [ "$code" -ne 0 ]; then
    echo "[launcher] ao3-hub exited with $code, not restarting" >&2
    exit "$code"
  fi
  echo "[launcher] ao3-hub exited cleanly (likely OTA), restarting in 1s…" >&2
  sleep 1
done
