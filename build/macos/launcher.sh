#!/bin/sh
# Startet die passende Version für Apple Silicon (arm64) oder Intel (amd64).
DIR="$(cd "$(dirname "$0")" && pwd)"
case "$(uname -m)" in
  arm64) exec "$DIR/StickTresor-arm64" "$@" ;;
  *)     exec "$DIR/StickTresor-amd64" "$@" ;;
esac
