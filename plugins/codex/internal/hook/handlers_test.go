package hook

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/grafana/sigil-sdk/go/sigil"

	"github.com/grafana/sigil-sdk/plugins/codex/internal/config"
	"github.com/grafana/sigil-sdk/plugins/codex/internal/fragment"
	"github.com/grafana/sigil-sdk/plugins/codex/internal/mapper"
	"github.com/grafana/sigil-sdk/plugins/codex/internal/redact"
)

func TestSessionStartWithoutTurnIDSeedsLaterTurn(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	logger := log.New(io.Discard, "", 0)
	cfg := config.Config{ContentCapture: sigil.ContentCaptureModeMetadataOnly}

	SessionStart(Payload{
		HookEventName:  "SessionStart",
		SessionID:      "sess",
		CWD:            "/repo",
		Model:          "gpt-5.5",
		Source:         "startup",
		TranscriptPath: "/tmp/transcript.jsonl",
		Timestamp:      "2026-05-11T10:00:00Z",
	}, cfg, logger)

	UserPromptSubmit(Payload{
		HookEventName: "UserPromptSubmit",
		SessionID:     "sess",
		TurnID:        "turn",
		Timestamp:     "2026-05-11T10:00:01Z",
	}, cfg, logger)

	got := fragment.LoadTolerant("sess", "turn", logger)
	if got == nil {
		t.Fatal("expected turn fragment")
	}
	if got.Cwd != "/repo" || got.Model != "gpt-5.5" || got.Source != "startup" || got.TranscriptPath != "/tmp/transcript.jsonl" {
		t.Fatalf("session defaults not inherited: %+v", got)
	}
}

func TestSessionStartRecordsSubagentLink(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	logger := log.New(io.Discard, "", 0)
	cfg := config.Config{ContentCapture: sigil.ContentCaptureModeMetadataOnly}
	transcriptPath := writeHookTranscript(t, `{"type":"session_meta","payload":{"id":"child","thread_source":"subagent","agent_role":"reviewer","agent_nickname":"Dalton","source":{"subagent":{"thread_spawn":{"parent_thread_id":"parent","depth":1}}}}}`)

	SessionStart(Payload{
		HookEventName:  "SessionStart",
		SessionID:      "child",
		Source:         "startup",
		TranscriptPath: transcriptPath,
	}, cfg, logger)

	got := fragment.LoadSubagentLinkTolerant("child", logger)
	if got == nil {
		t.Fatal("expected subagent link")
	}
	if got.ParentSessionID != "parent" || got.AgentRole != "reviewer" || got.AgentNickname != "Dalton" || got.AgentDepth != 1 || got.Source != "transcript.session_meta" {
		t.Fatalf("unexpected link: %+v", got)
	}
}

func TestRedactSpanContent(t *testing.T) {
	secret := "glc_abcdefghijklmnopqrstuvwxyz"
	raw, _ := json.Marshal(map[string]string{"token": secret})
	got := redactSpanContent(redact.New(), raw)
	if strings.Contains(got, secret) {
		t.Fatalf("unredacted secret in span content: %s", got)
	}
	if !strings.Contains(got, "[REDACTED:") {
		t.Fatalf("missing redaction marker: %s", got)
	}
}

func TestRedactSpanContentRedactsSensitiveJSONKeys(t *testing.T) {
	secret := "npm_" + strings.Repeat("A", 36)
	raw := json.RawMessage(`{"Authorization":"Bearer short","clientSecret":"short-secret","token":"` + secret + `","safe":"visible"}`)
	got := redactSpanContent(redact.New(), raw)
	for _, secret := range []string{"Bearer short", "short-secret", secret} {
		if strings.Contains(got, secret) {
			t.Fatalf("secret %q leaked in span content: %s", secret, got)
		}
	}
	if !strings.Contains(got, "[REDACTED:json-secret-field]") {
		t.Fatalf("missing sensitive key redaction marker: %s", got)
	}
	if !strings.Contains(got, "visible") {
		t.Fatalf("safe value should remain visible: %s", got)
	}
}

func TestPostToolUseLeavesStatusUnknownWhenCodexDoesNotProvideOne(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	logger := log.New(io.Discard, "", 0)
	cfg := config.Config{ContentCapture: sigil.ContentCaptureModeMetadataOnly}

	PostToolUse(Payload{
		HookEventName: "PostToolUse",
		SessionID:     "sess",
		TurnID:        "turn",
		ToolName:      "Bash",
		ToolUseID:     "tool-1",
		ToolResponse:  json.RawMessage(`{"output":"ok"}`),
	}, cfg, logger)

	got := fragment.LoadTolerant("sess", "turn", logger)
	if got == nil || len(got.Tools) != 1 {
		t.Fatalf("expected one tool record, got %+v", got)
	}
	if got.Tools[0].Status != "" {
		t.Fatalf("Status = %q, want unknown/empty", got.Tools[0].Status)
	}
}

func TestPostToolUseInfersKnownFailureShapes(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	logger := log.New(io.Discard, "", 0)
	cfg := config.Config{ContentCapture: sigil.ContentCaptureModeMetadataOnly}

	PostToolUse(Payload{
		HookEventName: "PostToolUse",
		SessionID:     "sess",
		TurnID:        "turn",
		ToolName:      "Bash",
		ToolUseID:     "tool-1",
		ToolResponse:  json.RawMessage(`{"metadata":{"exit_code":2}}`),
	}, cfg, logger)

	got := fragment.LoadTolerant("sess", "turn", logger)
	if got == nil || len(got.Tools) != 1 {
		t.Fatalf("expected one tool record, got %+v", got)
	}
	if got.Tools[0].Status != "error" {
		t.Fatalf("Status = %q, want error", got.Tools[0].Status)
	}
}

func TestPostToolUseInfersStatusFromToolOutputFallback(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	logger := log.New(io.Discard, "", 0)
	cfg := config.Config{ContentCapture: sigil.ContentCaptureModeMetadataOnly}

	PostToolUse(Payload{
		HookEventName: "PostToolUse",
		SessionID:     "sess",
		TurnID:        "turn",
		ToolName:      "Bash",
		ToolUseID:     "tool-1",
		ToolOutput:    json.RawMessage(`{"exitCode":1}`),
	}, cfg, logger)

	got := fragment.LoadTolerant("sess", "turn", logger)
	if got == nil || len(got.Tools) != 1 {
		t.Fatalf("expected one tool record, got %+v", got)
	}
	if got.Tools[0].Status != "error" {
		t.Fatalf("Status = %q, want error", got.Tools[0].Status)
	}
}

func TestPostToolUseDropsErrorMessageOutsideFullMode(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	logger := log.New(io.Discard, "", 0)
	cfg := config.Config{ContentCapture: sigil.ContentCaptureModeMetadataOnly}

	PostToolUse(Payload{
		HookEventName: "PostToolUse",
		SessionID:     "sess",
		TurnID:        "turn",
		ToolName:      "Bash",
		ToolUseID:     "tool-1",
		Error:         json.RawMessage(`{"message":"password=hunter2"}`),
	}, cfg, logger)

	got := fragment.LoadTolerant("sess", "turn", logger)
	if got == nil || len(got.Tools) != 1 {
		t.Fatalf("expected one tool record, got %+v", got)
	}
	if got.Tools[0].ErrorMessage != "" {
		t.Fatalf("ErrorMessage = %q, want empty outside full mode", got.Tools[0].ErrorMessage)
	}
	if got.Tools[0].Status != "error" {
		t.Fatalf("Status = %q, want error", got.Tools[0].Status)
	}
}

func TestPostToolUseRedactsErrorMessageInFullMode(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	logger := log.New(io.Discard, "", 0)
	cfg := config.Config{ContentCapture: sigil.ContentCaptureModeFull}

	PostToolUse(Payload{
		HookEventName: "PostToolUse",
		SessionID:     "sess",
		TurnID:        "turn",
		ToolName:      "Bash",
		ToolUseID:     "tool-1",
		Error:         json.RawMessage(`{"message":"password=hunter2"}`),
	}, cfg, logger)

	got := fragment.LoadTolerant("sess", "turn", logger)
	if got == nil || len(got.Tools) != 1 {
		t.Fatalf("expected one tool record, got %+v", got)
	}
	if strings.Contains(got.Tools[0].ErrorMessage, "hunter2") {
		t.Fatalf("ErrorMessage leaked secret: %q", got.Tools[0].ErrorMessage)
	}
	if !strings.Contains(got.Tools[0].ErrorMessage, "[REDACTED:") {
		t.Fatalf("ErrorMessage missing redaction marker: %q", got.Tools[0].ErrorMessage)
	}
}

func TestStopSuccessfulExportDeletesFragmentAndUsesAuth(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("SIGIL_OTEL_EXPORTER_OTLP_ENDPOINT", "")
	logger := log.New(io.Discard, "", 0)
	var gotPath, gotAuth string
	var requestCount atomic.Int64
	server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	t.Setenv("SIGIL_ENDPOINT", server.URL)
	t.Setenv("SIGIL_AUTH_TENANT_ID", "tenant")
	t.Setenv("SIGIL_AUTH_TOKEN", "token")

	if err := fragment.Update("sess", "turn", logger, func(f *fragment.Fragment) bool {
		f.Model = "gpt-5.5"
		f.Prompt = "hello"
		return true
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	Stop(Payload{HookEventName: "Stop", SessionID: "sess", TurnID: "turn"}, config.Config{ContentCapture: sigil.ContentCaptureModeFull}, logger)

	if got := fragment.LoadTolerant("sess", "turn", logger); got != nil {
		t.Fatalf("expected fragment deleted after successful export, got %+v", got)
	}
	if requestCount.Load() != 1 {
		t.Fatalf("request count = %d, want 1", requestCount.Load())
	}
	if gotPath != "/api/v1/generations:export" {
		t.Fatalf("path = %q", gotPath)
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("tenant:token"))
	if gotAuth != wantAuth {
		t.Fatalf("Authorization = %q, want %q", gotAuth, wantAuth)
	}
}

func TestStopResolvesSubagentParentGeneration(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("SIGIL_OTEL_EXPORTER_OTLP_ENDPOINT", "")
	logger := log.New(io.Discard, "", 0)
	var requestCount atomic.Int64
	server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()
	t.Setenv("SIGIL_ENDPOINT", server.URL)
	t.Setenv("SIGIL_AUTH_TENANT_ID", "tenant")
	t.Setenv("SIGIL_AUTH_TOKEN", "token")
	cfg := config.Config{ContentCapture: sigil.ContentCaptureModeMetadataOnly}

	parentTranscript := writeHookTranscript(t,
		`{"type":"session_meta","payload":{"id":"parent"}}`,
		`{"type":"turn_context","payload":{"turn_id":"parent-turn"}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"spawn_agent","call_id":"call_1"}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call_1","output":"{\"agent_id\":\"child\",\"nickname\":\"Dalton\"}"}}`,
	)
	childTranscript := writeHookTranscript(t, `{"type":"session_meta","payload":{"id":"child","thread_source":"subagent","agent_role":"reviewer","source":{"subagent":{"thread_spawn":{"parent_thread_id":"parent","depth":1}}}}}`)

	SessionStart(Payload{HookEventName: "SessionStart", SessionID: "parent", TranscriptPath: parentTranscript}, cfg, logger)
	SessionStart(Payload{HookEventName: "SessionStart", SessionID: "child", TranscriptPath: childTranscript}, cfg, logger)
	UserPromptSubmit(Payload{HookEventName: "UserPromptSubmit", SessionID: "child", TurnID: "child-turn"}, cfg, logger)

	Stop(Payload{HookEventName: "Stop", SessionID: "child", TurnID: "child-turn", Timestamp: "2026-05-11T10:00:00Z"}, cfg, logger)

	if requestCount.Load() != 1 {
		t.Fatalf("request count = %d, want 1", requestCount.Load())
	}
	got := fragment.LoadSubagentLinkTolerant("child", logger)
	if got == nil {
		t.Fatal("expected subagent link retained")
	}
	if got.ParentTurnID != "parent-turn" || got.ParentGenerationID != mapper.GenerationID("parent", "parent-turn") || got.SpawnCallID != "call_1" || got.AgentNickname != "Dalton" {
		t.Fatalf("unexpected resolved link: %+v", got)
	}
	if got := fragment.LoadTolerant("child", "child-turn", logger); got != nil {
		t.Fatalf("expected exported child fragment deleted, got %+v", got)
	}
}

func TestStopMissingCredentialsDiscardsFragment(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("SIGIL_ENDPOINT", "")
	t.Setenv("SIGIL_AUTH_TENANT_ID", "")
	t.Setenv("SIGIL_AUTH_TOKEN", "")
	logger := log.New(io.Discard, "", 0)
	if err := fragment.Update("sess", "turn", logger, func(f *fragment.Fragment) bool {
		f.Model = "gpt-5.5"
		return true
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	Stop(Payload{HookEventName: "Stop", SessionID: "sess", TurnID: "turn"}, config.Config{ContentCapture: sigil.ContentCaptureModeMetadataOnly}, logger)

	if got := fragment.LoadTolerant("sess", "turn", logger); got != nil {
		t.Fatalf("expected fragment discarded without credentials, got %+v", got)
	}
}

func TestStopExportFailureRetainsFragmentAndBoundsRetries(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("SIGIL_OTEL_EXPORTER_OTLP_ENDPOINT", "")
	logger := log.New(io.Discard, "", 0)
	var requestCount atomic.Int64
	server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer server.Close()
	t.Setenv("SIGIL_ENDPOINT", server.URL)
	t.Setenv("SIGIL_AUTH_TENANT_ID", "tenant")
	t.Setenv("SIGIL_AUTH_TOKEN", "token")

	if err := fragment.Update("sess", "turn", logger, func(f *fragment.Fragment) bool {
		f.Model = "gpt-5.5"
		return true
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	start := time.Now()
	Stop(Payload{HookEventName: "Stop", SessionID: "sess", TurnID: "turn"}, config.Config{ContentCapture: sigil.ContentCaptureModeMetadataOnly}, logger)

	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Stop took %s on immediate export failure", elapsed)
	}
	if got := fragment.LoadTolerant("sess", "turn", logger); got == nil {
		t.Fatal("expected fragment retained after export failure")
	}
	if requestCount.Load() != 2 {
		t.Fatalf("request count = %d, want 2 attempts after bounded retry override", requestCount.Load())
	}
}

func newTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("loopback listen unavailable in this sandbox: %v", err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	return server
}

func writeHookTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	return path
}
