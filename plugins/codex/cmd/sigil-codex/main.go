package main

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/grafana/sigil-sdk/plugins/codex/internal/config"
	"github.com/grafana/sigil-sdk/plugins/codex/internal/fragment"
	"github.com/grafana/sigil-sdk/plugins/codex/internal/hook"
)

func main() {
	config.ApplyEnv(nil)
	logger := initLogger()
	defer recoverAndLog(logger)
	run(logger, os.Stdin)
}

func run(logger *log.Logger, stdin io.Reader) {
	raw, err := io.ReadAll(stdin)
	if err != nil {
		logger.Printf("dispatch: read stdin: %v", err)
		return
	}
	if strings.TrimSpace(string(raw)) == "" {
		logger.Print("dispatch: empty stdin")
		return
	}
	var payload hook.Payload
	if err := json.Unmarshal(raw, &payload); err != nil {
		logger.Printf("dispatch: invalid JSON: %v", err)
		return
	}
	if payload.HookEventName == "" {
		logger.Print("dispatch: missing hook_event_name")
		return
	}
	defer func() {
		fragment.CleanupStaleExcept(fragment.DefaultStaleAge, time.Now(), logger, payload.SessionID, payload.TurnID)
	}()
	cfg := config.Load(logger)
	logger.Printf("dispatch: event=%s", payload.HookEventName)
	switch payload.HookEventName {
	case "SessionStart":
		hook.SessionStart(payload, cfg, logger)
	case "UserPromptSubmit":
		hook.UserPromptSubmit(payload, cfg, logger)
	case "PostToolUse":
		hook.PostToolUse(payload, cfg, logger)
	case "Stop":
		hook.Stop(payload, cfg, logger)
	default:
		logger.Printf("dispatch: unknown event %q", payload.HookEventName)
	}
}

func initLogger() *log.Logger {
	logger := log.New(io.Discard, "sigil-codex: ", log.Ltime)
	if !parseBool(os.Getenv("SIGIL_DEBUG")) {
		return logger
	}
	path := fragment.LogFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return logger
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return logger
	}
	return log.New(f, "sigil-codex: ", log.Ldate|log.Ltime|log.Lmicroseconds)
}

func parseBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func recoverAndLog(logger *log.Logger) {
	if r := recover(); r != nil {
		logger.Printf("dispatch: panic: %v", r)
	}
}
