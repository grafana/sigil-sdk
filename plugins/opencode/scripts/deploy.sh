#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PLUGIN_DIR="$(dirname "$SCRIPT_DIR")"
OPENCODE_DIR="${HOME}/.config/opencode"

echo "Deploying opencode-agento11y..."

mkdir -p "${OPENCODE_DIR}/plugins" "${OPENCODE_DIR}/skills"

# Clean up installs from before the sigil -> agento11y rename.
rm -f "${OPENCODE_DIR}/plugins/opencode-sigil.js"
rm -rf "${OPENCODE_DIR}/skills/sigil"

ln -sf "${PLUGIN_DIR}/dist/index.js" "${OPENCODE_DIR}/plugins/opencode-agento11y.js"
echo "  [link] opencode-agento11y.js"

if [ -d "${PLUGIN_DIR}/skills/agento11y" ]; then
  rm -rf "${OPENCODE_DIR}/skills/agento11y"
  cp -R "${PLUGIN_DIR}/skills/agento11y" "${OPENCODE_DIR}/skills/agento11y"
  echo "  [copy] agento11y skill"
fi

echo "Done. Restart OpenCode to pick up changes."
