#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SDK_DIR="$(cd "${SCRIPT_DIR}/../../../js" && pwd)"

# Build the SDK so workspace-linked types are available
if [ ! -f "${SDK_DIR}/dist/index.d.ts" ]; then
  echo "Building @grafana/sigil-sdk-js..."
  npx tsc --project "${SDK_DIR}/tsconfig.build.json"
fi

tsc --noEmit

npx esbuild src/index.ts \
  --bundle \
  --format=esm \
  --platform=node \
  --target=es2022 \
  --outfile=dist/index.js \
  --external:@opencode-ai/plugin \
  --external:@opencode-ai/sdk

tsc --project tsconfig.build.json --emitDeclarationOnly --declaration --declarationMap --outDir dist
