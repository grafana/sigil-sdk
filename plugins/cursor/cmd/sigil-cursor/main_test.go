package main

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

// runDispatch is a small test helper that invokes the package's run() with a
// captured stdout buffer.
func runDispatch(t *testing.T, stdin string) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var stdout bytes.Buffer
	logger := log.New(&bytes.Buffer{}, "", 0)
	run(logger, strings.NewReader(stdin), &stdout)
	return stdout.String()
}

// If beforeSubmitPrompt returns without writing the permissive response to
// stdout, Cursor blocks the user's prompt. We use defer to guarantee the
// response fires on every code path.
func TestRun_BeforeSubmitPrompt_AlwaysEmitsPermissive(t *testing.T) {
	cases := []struct {
		name  string
		stdin string
	}{
		{
			name:  "valid payload",
			stdin: `{"hook_event_name":"beforeSubmitPrompt","conversation_id":"c","generation_id":"g","prompt":"hi"}`,
		},
		{
			name:  "invalid JSON but discriminator readable",
			stdin: `{"hook_event_name":"beforeSubmitPrompt","prompt":NOPE}`,
		},
		{
			name:  "missing IDs",
			stdin: `{"hook_event_name":"beforeSubmitPrompt"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := runDispatch(t, tc.stdin)
			if !strings.Contains(out, `"continue":true`) || !strings.Contains(out, `"permission":"allow"`) {
				t.Errorf("missing permissive response on beforeSubmitPrompt; got %q", out)
			}
		})
	}
}

func TestRun_NonBeforeEventsEmitNothing(t *testing.T) {
	out := runDispatch(t, `{"hook_event_name":"afterAgentResponse","conversation_id":"c","generation_id":"g","text":"hi"}`)
	if out != "" {
		t.Errorf("non-before event should produce no stdout; got %q", out)
	}
}

func TestRun_EmptyStdinDoesNotBlock(t *testing.T) {
	out := runDispatch(t, "")
	// No event name → no permissive response. Empty stdin shouldn't write
	// anything at all.
	if out != "" {
		t.Errorf("empty stdin should produce no stdout; got %q", out)
	}
}

func TestRun_MissingHookEventNameLogsAndReturns(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var logBuf bytes.Buffer
	logger := log.New(&logBuf, "", 0)
	run(logger, strings.NewReader(`{"foo":"bar"}`), &bytes.Buffer{})
	if !strings.Contains(logBuf.String(), "missing hook_event_name") {
		t.Errorf("expected 'missing hook_event_name' log; got %q", logBuf.String())
	}
}

func TestRun_UnknownEventLogsAndReturns(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var logBuf bytes.Buffer
	logger := log.New(&logBuf, "", 0)
	run(logger, strings.NewReader(`{"hook_event_name":"unknownEvent"}`), &bytes.Buffer{})
	if !strings.Contains(logBuf.String(), "unknown event") {
		t.Errorf("expected 'unknown event' log; got %q", logBuf.String())
	}
}
