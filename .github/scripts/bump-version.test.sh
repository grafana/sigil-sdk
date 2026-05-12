#!/usr/bin/env bash
# Tests for bump-version.sh. Exits non-zero on any failure.

set -uo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
BUMP="${DIR}/bump-version.sh"

fail=0
pass=0

assert_ok() {
  local desc="$1" want="$2"; shift 2
  local got
  got=$("$BUMP" "$@" 2>/dev/null) || {
    echo "FAIL ${desc}: $* exited non-zero"
    fail=$((fail + 1)); return
  }
  if [[ "$got" == "$want" ]]; then
    pass=$((pass + 1))
  else
    echo "FAIL ${desc}: $* -> '${got}', want '${want}'"
    fail=$((fail + 1))
  fi
}

assert_rejects() {
  local desc="$1"; shift
  if "$BUMP" "$@" >/dev/null 2>&1; then
    echo "FAIL ${desc}: $* was accepted (should have been rejected)"
    fail=$((fail + 1))
  else
    pass=$((pass + 1))
  fi
}

# Happy paths.
assert_ok "patch"            "0.3.1"          patch 0.3.0
assert_ok "minor"            "0.4.0"          minor 0.3.0
assert_ok "major"            "1.0.0"          major 0.3.0
assert_ok "patch from zero"  "0.0.1"          patch 0.0.0
assert_ok "minor from zero"  "0.1.0"          minor 0.0.0
assert_ok "major from zero"  "1.0.0"          major 0.0.0
assert_ok "minor resets patch"  "1.3.0"       minor 1.2.5
assert_ok "major resets both"   "2.0.0"       major 1.2.5
assert_ok "large numbers"    "999.999.1000"   patch 999.999.999

# Rejected inputs.
assert_rejects "no args"
assert_rejects "missing current"           patch
assert_rejects "bad bump type"             wibble 1.0.0
assert_rejects "missing patch component"   patch 1.0
assert_rejects "extra component"           patch 1.0.0.0
assert_rejects "prerelease suffix"         patch 1.0.0-rc1
assert_rejects "build metadata suffix"     patch 1.0.0+abc
assert_rejects "leading zero patch"        patch 0.0.08
assert_rejects "leading zero minor"        patch 0.09.0
assert_rejects "leading zero major"        patch 01.0.0
assert_rejects "v prefix"                  patch v1.0.0
assert_rejects "leading whitespace"        patch " 1.0.0"
assert_rejects "trailing garbage"          patch "1.0.0x"
assert_rejects "negative"                  patch "-1.0.0"
assert_rejects "empty current"             patch ""

echo "passed: ${pass}, failed: ${fail}"
[[ $fail -eq 0 ]]
