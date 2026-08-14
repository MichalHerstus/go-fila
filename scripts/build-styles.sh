#!/usr/bin/env bash
# build-styles.sh — regenerate the embedded pre-built dashboard stylesheet
# (internal/generator/assets/styles.css) from the kitchen-sink fixture.
#
# Dev workflow only: the generated dashboard build itself never runs Tailwind.
# The script builds the yaga binary, generates a kitchen-sink project offline,
# compiles its templates with the pinned Tailwind standalone binary (downloaded
# to ~/.cache on first run; a "tailwindcss" on PATH is preferred), and copies
# the minified result into the embedded asset.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CACHE_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/yaga/tailwind"
VERSION="v3.4.19"

case "$(uname -s)-$(uname -m)" in
  Linux-x86_64)  FILE="tailwindcss-linux-x64" ;;
  Linux-aarch64|Linux-arm64) FILE="tailwindcss-linux-arm64" ;;
  Darwin-x86_64) FILE="tailwindcss-macos-x64" ;;
  Darwin-arm64)  FILE="tailwindcss-macos-arm64" ;;
  *) echo "build-styles: unsupported platform $(uname -s)-$(uname -m)" >&2; exit 1 ;;
esac

if command -v tailwindcss >/dev/null 2>&1; then
  BIN="$(command -v tailwindcss)"
else
  mkdir -p "$CACHE_DIR"
  BIN="$CACHE_DIR/tailwindcss"
  if [ ! -x "$BIN" ]; then
    echo "build-styles: downloading tailwindcss $VERSION ($FILE)..."
    curl -sL -o "$BIN" "https://github.com/tailwindlabs/tailwindcss/releases/download/$VERSION/$FILE"
    chmod +x "$BIN"
  fi
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "build-styles: generating kitchen-sink project..."
(cd "$ROOT" && go build -o "$TMP/yaga" ./cmd/yaga)
"$TMP/yaga" generate --config "$ROOT/testdata/kitchen.yaml" --out "$TMP/kitchen" --force

echo "build-styles: compiling CSS with tailwindcss ($("$BIN" --version 2>/dev/null || echo unknown))..."
cp "$ROOT/scripts/styles.tailwind.config.js" "$TMP/kitchen/tailwind.config.js"
cat > "$TMP/kitchen/input.css" <<'EOF'
@tailwind base;
@tailwind components;
@tailwind utilities;
EOF
cd "$TMP/kitchen"
"$BIN" -i input.css -o "$TMP/styles.css" -c tailwind.config.js --minify

mkdir -p "$ROOT/internal/generator/assets"
cp "$TMP/styles.css" "$ROOT/internal/generator/assets/styles.css"
echo "build-styles: wrote $(wc -c < "$ROOT/internal/generator/assets/styles.css") bytes to internal/generator/assets/styles.css"
