#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PLUGIN_DIR="$(dirname "$SCRIPT_DIR")"
OPENCODE_DIR="${HOME}/.config/opencode"

echo "Deploying opencode-sigil..."

mkdir -p "${OPENCODE_DIR}/plugins" "${OPENCODE_DIR}/skills"

ln -sf "${PLUGIN_DIR}/dist/index.js" "${OPENCODE_DIR}/plugins/opencode-sigil.js"
echo "  [link] opencode-sigil.js"

if [ -d "${PLUGIN_DIR}/skills/sigil" ]; then
  rm -rf "${OPENCODE_DIR}/skills/sigil"
  cp -R "${PLUGIN_DIR}/skills/sigil" "${OPENCODE_DIR}/skills/sigil"
  echo "  [copy] sigil skill"
fi

echo "Done. Restart OpenCode to pick up changes."
