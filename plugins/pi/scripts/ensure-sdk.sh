#!/usr/bin/env bash
# Build the workspace SDK so its dist/ is available for typecheck/test/build.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SDK_DIR="$(cd "${SCRIPT_DIR}/../../../js" && pwd)"

if [ ! -f "${SDK_DIR}/dist/index.d.ts" ]; then
  echo "Building @grafana/sigil-sdk-js..."
  (cd "${SDK_DIR}" && npx tsc --project tsconfig.build.json)
fi
