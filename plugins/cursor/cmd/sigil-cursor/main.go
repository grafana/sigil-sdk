// Command sigil-cursor is the entry point for the Cursor plugin's hook
// runtime. It reads one JSON payload from stdin, dispatches to the matching
// handler, and never crashes Cursor — telemetry failures must always be
// silent.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/grafana/sigil-sdk/plugins/cursor/internal/config"
	"github.com/grafana/sigil-sdk/plugins/cursor/internal/fragment"
	"github.com/grafana/sigil-sdk/plugins/cursor/internal/hook"
)

// permissiveResponse is the JSON Cursor expects on `before*` events when the
// plugin wants to allow the action. Cursor treats *missing* output on those
// events as "block" — so we always emit this for beforeSubmitPrompt, no
// matter how the handler exits.
const permissiveResponse = `{"continue":true,"permission":"allow"}` + "\n"

// beforeSubmitMarker is the exact substring we look for in raw stdin to
// decide whether to write the permissive response. Cursor emits compact JSON,
// so a substring match suffices — and it fires even if json.Unmarshal fails
// or a later panic interrupts dispatch.
var beforeSubmitMarker = []byte(`"hook_event_name":"beforeSubmitPrompt"`)

func main() {
	logger := initLogger()
	defer recoverAndLog(logger)
	run(logger, os.Stdin, os.Stdout)
}

// run is separated from main() so tests can drive it with a custom stdin /
// stdout / logger.
func run(logger *log.Logger, stdin io.Reader, stdout io.Writer) {
	var raw []byte
	defer func() {
		if bytes.Contains(raw, beforeSubmitMarker) {
			_, _ = fmt.Fprint(stdout, permissiveResponse)
		}
	}()

	var err error
	raw, err = io.ReadAll(stdin)
	if err != nil {
		logger.Printf("dispatch: read stdin: %v", err)
		return
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		logger.Print("dispatch: empty stdin")
		return
	}

	var payload hook.Payload
	if err := json.Unmarshal(raw, &payload); err != nil {
		logger.Printf("dispatch: invalid JSON: %v", err)
		return
	}
	event := payload.HookEventName
	if event == "" {
		logger.Print("dispatch: missing hook_event_name")
		return
	}
	logger.Printf("dispatch: event=%s", event)

	cfg := config.Load(logger)

	switch event {
	case "sessionStart":
		hook.SessionStart(payload, logger)
	case "beforeSubmitPrompt":
		hook.BeforeSubmit(payload, logger)
	case "afterAgentResponse":
		hook.AfterAgentResponse(payload, logger)
	case "afterAgentThought":
		hook.AfterAgentThought(payload, logger)
	case "postToolUse":
		hook.PostToolUse(payload, cfg, logger, false)
	case "postToolUseFailure":
		hook.PostToolUse(payload, cfg, logger, true)
	case "stop":
		hook.Stop(payload, cfg, logger)
	case "sessionEnd":
		hook.SessionEnd(payload, cfg, logger)
	default:
		logger.Printf("dispatch: unknown event %q", event)
	}
}

// initLogger wires the log destination based on SIGIL_DEBUG. The default is
// /dev/null — hooks must not surface anything to stderr/stdout under any
// circumstance because Cursor reads stdout for the hook's response.
//
// SIGIL_DEBUG is checked in the OS env first, then in the dotenv file
// (Cursor's hook process strips most env, so the dotenv is often the only
// place users can reliably set it).
func initLogger() *log.Logger {
	logger := log.New(io.Discard, "sigil-cursor: ", log.Ltime)
	if !config.BoolEnv("SIGIL_DEBUG", config.LoadDotenv(config.FilePath(), nil)) {
		return logger
	}
	path := fragment.LogFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return logger
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return logger
	}
	return log.New(f, "sigil-cursor: ", log.Ldate|log.Ltime|log.Lmicroseconds)
}

// recoverAndLog catches panics so Cursor never sees a non-zero exit (which
// would surface as a failed-hook indicator). Logging is best-effort.
func recoverAndLog(logger *log.Logger) {
	if r := recover(); r != nil {
		logger.Printf("dispatch: panic: %v", r)
	}
}
