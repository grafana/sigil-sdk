package sigilemit

import (
	"testing"
	"time"

	"github.com/grafana/sigil-sdk/go/sigil"
)

func TestExportEndpoint(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		want     string
	}{
		{"no trailing slash", "http://localhost:8080", "http://localhost:8080/api/v1/generations:export"},
		{"trailing slash trimmed", "http://localhost:8080/", "http://localhost:8080/api/v1/generations:export"},
		{"multiple trailing slashes trimmed", "http://localhost:8080///", "http://localhost:8080/api/v1/generations:export"},
		{"empty endpoint", "", "/api/v1/generations:export"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SIGIL_ENDPOINT", tc.endpoint)
			if got := ExportEndpoint(); got != tc.want {
				t.Fatalf("ExportEndpoint() = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestNewClientExportHook(t *testing.T) {
	t.Setenv("SIGIL_ENDPOINT", "http://localhost:8080/")
	// copilot/cursor leave basic-auth credentials to the SDK's env resolution;
	// the SDK validates that basic_password is present, so provide them.
	t.Setenv("SIGIL_AUTH_TENANT_ID", "tenant")
	t.Setenv("SIGIL_AUTH_TOKEN", "token")

	var got sigil.GenerationExportConfig
	client := NewClient(ClientOptions{
		InstrumentationName: "sigil.test",
		ContentCapture:      sigil.ContentCaptureModeMetadataOnly,
		Export: func(e *sigil.GenerationExportConfig) {
			got = *e // capture base config the builder produced
			e.BatchSize = 100
		},
	})
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	t.Cleanup(func() { _ = client.Shutdown(t.Context()) })

	if got.Protocol != sigil.GenerationExportProtocolHTTP {
		t.Errorf("base Protocol = %v; want HTTP", got.Protocol)
	}
	if got.Endpoint != "http://localhost:8080/api/v1/generations:export" {
		t.Errorf("base Endpoint = %q", got.Endpoint)
	}
	if got.Auth.Mode != sigil.ExportAuthModeBasic {
		t.Errorf("base Auth.Mode = %v; want Basic", got.Auth.Mode)
	}
}

func TestNewClientWithoutExportHook(t *testing.T) {
	t.Setenv("SIGIL_ENDPOINT", "http://localhost:8080")
	t.Setenv("SIGIL_AUTH_TENANT_ID", "tenant")
	t.Setenv("SIGIL_AUTH_TOKEN", "token")
	client := NewClient(ClientOptions{InstrumentationName: "sigil.test"})
	if client == nil {
		t.Fatal("NewClient returned nil")
	}
	_ = client.Shutdown(t.Context())
}

func TestToolSpanWindow(t *testing.T) {
	genEnd := time.Date(2026, 4, 28, 12, 0, 30, 0, time.UTC)
	dur := func(ms float64) *float64 { return &ms }

	cases := []struct {
		name          string
		completedAt   string
		duration      *float64
		wantStarted   time.Time
		wantCompleted time.Time
	}{
		{
			name:          "completedAt minus duration",
			completedAt:   "2026-04-28T12:00:10.500Z",
			duration:      dur(2500),
			wantStarted:   time.Date(2026, 4, 28, 12, 0, 8, 0, time.UTC),
			wantCompleted: time.Date(2026, 4, 28, 12, 0, 10, 500_000_000, time.UTC),
		},
		{
			name:          "no duration → started equals completed",
			completedAt:   "2026-04-28T12:00:10Z",
			duration:      nil,
			wantStarted:   time.Date(2026, 4, 28, 12, 0, 10, 0, time.UTC),
			wantCompleted: time.Date(2026, 4, 28, 12, 0, 10, 0, time.UTC),
		},
		{
			name:          "missing completedAt falls back to genCompletedAt",
			completedAt:   "",
			duration:      dur(1000),
			wantStarted:   genEnd.Add(-1000 * time.Millisecond),
			wantCompleted: genEnd,
		},
		{
			name:          "unparseable completedAt falls back to genCompletedAt",
			completedAt:   "not-a-timestamp",
			duration:      dur(500),
			wantStarted:   genEnd.Add(-500 * time.Millisecond),
			wantCompleted: genEnd,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotStart, gotEnd := ToolSpanWindow(tc.completedAt, tc.duration, genEnd)
			if !gotStart.Equal(tc.wantStarted) {
				t.Errorf("startedAt = %s; want %s", gotStart, tc.wantStarted)
			}
			if !gotEnd.Equal(tc.wantCompleted) {
				t.Errorf("completedAt = %s; want %s", gotEnd, tc.wantCompleted)
			}
		})
	}
}

func TestToolError(t *testing.T) {
	cases := []struct {
		name string
		msg  string
		want string
	}{
		{"empty message → sentinel", "", "tool returned error"},
		{"non-empty message preserved", "boom", "boom"},
		{"whitespace message preserved verbatim", "   ", "   "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ToolError(tc.msg).Error(); got != tc.want {
				t.Errorf("ToolError(%q) = %q; want %q", tc.msg, got, tc.want)
			}
		})
	}
}
