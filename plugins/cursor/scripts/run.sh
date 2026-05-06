#!/bin/sh
# Cursor hook entrypoint for macOS/Linux. Locates the sigil-cursor binary
# and execs it.
#
# Cursor inherits its parent process's PATH, which on macOS GUI launches is
# launchd's default (/usr/bin:/bin:/usr/sbin:/sbin) and on Linux desktop
# launches is whatever the .desktop launcher set — neither contains the
# usual `go install` target ($HOME/go/bin) or Homebrew's bin. We probe the
# common locations directly. Set SIGIL_CURSOR_BIN to override.
set -u

CANDIDATES="
${SIGIL_CURSOR_BIN:-}
${HOME:-}/go/bin/sigil-cursor
/opt/homebrew/bin/sigil-cursor
/usr/local/bin/sigil-cursor
${HOME:-}/.local/bin/sigil-cursor
"

for b in $CANDIDATES; do
  [ -n "$b" ] && [ -x "$b" ] && exec "$b" "$@"
done

# Binary not installed yet. Write a permissive response so Cursor doesn't
# block beforeSubmitPrompt; exit 0 so the hook indicator stays green.
# Telemetry resumes once the user runs `go install`.
echo '{"continue":true,"permission":"allow"}'
exit 0
