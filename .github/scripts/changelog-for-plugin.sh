#!/usr/bin/env bash
# Generate a markdown changelog section for a plugin release.
#
# Usage:
#   changelog-for-plugin.sh <plugin-path> <new-version>
#
# Example:
#   .github/scripts/changelog-for-plugin.sh plugins/sigil 0.7.0
#
# Walks `git log <last-tag>..HEAD -- <plugin-path>/` and groups commits
# by their conventional-commit type. Squash-merge subjects already carry
# the `(#PR)` suffix, so the output is link-ready on GitHub.
#
# Prints the section (one ## heading + body) to stdout. Does not touch
# any files. Caller is responsible for prepending the output to
# CHANGELOG.md.

set -euo pipefail

PLUGIN="${1:-}"
NEW="${2:-}"

if [[ -z "$PLUGIN" || -z "$NEW" ]]; then
  echo "usage: $0 <plugin-path> <new-version>" >&2
  exit 64
fi

# Find the previous tag for this plugin. If none, walk full history.
PREV=$(git tag -l "${PLUGIN}/v*" | sort -V | tail -1 || true)
if [[ -n "$PREV" ]]; then
  RANGE="${PREV}..HEAD"
else
  RANGE="HEAD"
fi

# Only commits that actually touched files under the plugin path.
COMMITS=$(git log --no-merges --pretty=format:'%s' "$RANGE" -- "${PLUGIN}/" || true)

emit_section() {
  local title="$1" pattern="$2" matched
  matched=$(grep -E "$pattern" <<<"$COMMITS" || true)
  [[ -z "$matched" ]] && return
  printf '### %s\n\n' "$title"
  # Drop the leading `type` but keep the scope as a bold prefix when present,
  # so `feat(plugins/copilot): foo` -> `- **plugins/copilot**: foo` and a
  # bare `feat: foo` -> `- foo`.
  printf '%s\n' "$matched" \
    | sed -E 's/^[a-z]+\(([^)]+)\)!?: /- **\1**: /' \
    | sed -E 's/^[a-z]+!?: /- /'
  printf '\n'
}

printf '## [%s] - %s\n\n' "$NEW" "$(date +%Y-%m-%d)"

# Breaking changes first (any type with `!:` suffix). Keep them out of
# type-specific sections so each commit appears once.
emit_section 'Breaking Changes' '^[a-z]+(\([^)]+\))?!: '
emit_section 'Features'         '^feat(\([^)]+\))?: '
emit_section 'Bug Fixes'        '^fix(\([^)]+\))?: '
emit_section 'Performance'      '^perf(\([^)]+\))?: '
emit_section 'Documentation'    '^docs(\([^)]+\))?: '

if ! grep -qE '^([a-z]+(\([^)]+\))?!: |(feat|fix|perf|docs)(\([^)]+\))?: )' <<<"$COMMITS"; then
  printf '_No user-facing changes._\n\n'
fi
