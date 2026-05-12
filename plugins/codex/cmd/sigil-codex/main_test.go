package main

import (
	"io"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/grafana/sigil-sdk/plugins/codex/internal/fragment"
)

func TestRunSkipsCurrentTurnDuringStaleCleanup(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("SIGIL_CONTENT_CAPTURE_MODE", "full")
	logger := log.New(io.Discard, "", 0)
	if err := fragment.Update("sess", "turn", logger, func(f *fragment.Fragment) bool {
		f.Prompt = "old"
		return true
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(fragment.FragmentFilePath("sess", "turn"), old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	run(logger, strings.NewReader(`{
		"hook_event_name": "UserPromptSubmit",
		"session_id": "sess",
		"turn_id": "turn",
		"prompt": "new"
	}`))

	got := fragment.LoadTolerant("sess", "turn", logger)
	if got == nil {
		t.Fatal("current fragment was removed by stale cleanup")
	}
	if got.Prompt != "new" {
		t.Fatalf("Prompt = %q, want new", got.Prompt)
	}
}
