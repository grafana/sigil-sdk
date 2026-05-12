#!/usr/bin/env bash
# Bump a semver version.
#
# Usage:
#   bump-version.sh <patch|minor|major> <current X.Y.Z>
#
# Prints the new version on stdout. Exits non-zero on invalid input.
# Only plain X.Y.Z is supported (no pre-release or build metadata) since
# none of our SDKs publish anything else.

set -euo pipefail

BUMP="${1:-}"
CURRENT="${2:-}"

if [[ -z "$BUMP" || -z "$CURRENT" ]]; then
  echo "usage: $0 <patch|minor|major> <current>" >&2
  exit 64
fi

# Each component must be 0 or a non-zero digit followed by digits; this
# matches the semver spec and rules out leading zeros that would otherwise
# trip bash's octal arithmetic (e.g. 08).
NUM='(0|[1-9][0-9]*)'
if ! [[ "$CURRENT" =~ ^${NUM}\.${NUM}\.${NUM}$ ]]; then
  echo "invalid current version: ${CURRENT} (expected X.Y.Z)" >&2
  exit 65
fi

MAJOR="${BASH_REMATCH[1]}"
MINOR="${BASH_REMATCH[2]}"
PATCH="${BASH_REMATCH[3]}"

case "$BUMP" in
  major) MAJOR=$((MAJOR + 1)); MINOR=0; PATCH=0 ;;
  minor) MINOR=$((MINOR + 1)); PATCH=0 ;;
  patch) PATCH=$((PATCH + 1)) ;;
  *)
    echo "invalid bump type: ${BUMP} (expected patch|minor|major)" >&2
    exit 64
    ;;
esac

echo "${MAJOR}.${MINOR}.${PATCH}"
