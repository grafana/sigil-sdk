#!/usr/bin/env bash
set -euo pipefail

mise run build:ts:sdk-js

pnpm exec tsc --noEmit

pnpm exec esbuild src/index.ts \
  --bundle \
  --format=esm \
  --platform=node \
  --target=es2022 \
  --outfile=dist/index.js \
  --external:@opencode-ai/plugin \
  --external:@opencode-ai/sdk

pnpm exec tsc --project tsconfig.build.json --emitDeclarationOnly --declaration --declarationMap --outDir dist
