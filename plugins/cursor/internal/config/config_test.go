package config

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/grafana/sigil-sdk/go/sigil"
)

func TestResolveContentCapture(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want sigil.ContentCaptureMode
		warn bool
	}{
		{"empty falls back", "", sigil.ContentCaptureModeMetadataOnly, false},
		{"full", "full", sigil.ContentCaptureModeFull, false},
		{"metadata_only", "metadata_only", sigil.ContentCaptureModeMetadataOnly, false},
		{"no_tool_content", "no_tool_content", sigil.ContentCaptureModeNoToolContent, false},
		{"case insensitive", "FULL", sigil.ContentCaptureModeFull, false},
		{"default keyword", "default", sigil.ContentCaptureModeMetadataOnly, false},
		{"unknown warns and fails closed", "fll", sigil.ContentCaptureModeMetadataOnly, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := log.New(&buf, "", 0)
			got := resolveContentCapture(tc.raw, logger)
			if got != tc.want {
				t.Errorf("resolveContentCapture(%q) = %v; want %v", tc.raw, got, tc.want)
			}
			if tc.warn && !strings.Contains(buf.String(), "unknown") {
				t.Errorf("expected warning log for %q; got %q", tc.raw, buf.String())
			}
			if !tc.warn && buf.Len() > 0 {
				t.Errorf("unexpected log for %q: %q", tc.raw, buf.String())
			}
		})
	}
}

func TestLoadDotenv_AllVariants(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.env")
	body := `# leading comment
SIGIL_URL=https://sigil.example.com
export SIGIL_USER=alice
SIGIL_PASSWORD="secret with spaces"
SIGIL_CONTENT_CAPTURE_MODE='full'
SIGIL_EXTRA_TAGS=a=1,b=2  # inline comment
SIGIL_DEBUG=true
no_equals_line
=missingkey
EMPTY=
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := LoadDotenv(path, log.New(&bytes.Buffer{}, "", 0))
	want := map[string]string{
		"SIGIL_URL":                  "https://sigil.example.com",
		"SIGIL_USER":                 "alice",
		"SIGIL_PASSWORD":             "secret with spaces",
		"SIGIL_CONTENT_CAPTURE_MODE": "full",
		"SIGIL_EXTRA_TAGS":           "a=1,b=2",
		"SIGIL_DEBUG":                "true",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d (got=%v)", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("got[%q]=%q want %q", k, got[k], v)
		}
	}
	for badKey := range map[string]struct{}{"EMPTY": {}, "": {}, "no_equals_line": {}} {
		if _, ok := got[badKey]; ok {
			t.Errorf("malformed key %q should not be loaded", badKey)
		}
	}
}

// Quoted values keep inner content verbatim — no quote stripping breakage,
// no inline-comment chopping into the value, and `#` inside quotes is
// preserved.
func TestLoadDotenv_QuotedValueEdgeCases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.env")
	body := `DOUBLE="my secret" # comment
SINGLE='other secret' # comment
HASH_INSIDE="value # not a comment"
PLAIN_COMMENT=plain # trailing
SPACES_INSIDE="  has spaces  "
UNTERMINATED="oops
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := LoadDotenv(path, log.New(&bytes.Buffer{}, "", 0))

	cases := []struct{ key, want string }{
		{"DOUBLE", "my secret"},
		{"SINGLE", "other secret"},
		{"HASH_INSIDE", "value # not a comment"},
		{"PLAIN_COMMENT", "plain"},
		{"SPACES_INSIDE", "  has spaces  "},
		{"UNTERMINATED", `"oops`}, // unterminated quote → leave raw
	}
	for _, tc := range cases {
		if got[tc.key] != tc.want {
			t.Errorf("got[%q] = %q; want %q", tc.key, got[tc.key], tc.want)
		}
	}
}

func TestBoolEnv(t *testing.T) {
	cases := []struct {
		name    string
		envVal  string
		fileVal string
		want    bool
	}{
		{"os env true", "true", "", true},
		{"os env TRUE case-insensitive", "TRUE", "", true},
		{"os env false", "false", "", false},
		{"os env empty falls back to file true", "", "true", true},
		{"os env empty file false", "", "false", false},
		{"os env wins over file", "false", "true", false},
		{"both empty", "", "", false},
		{"random string", "yes", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("FAKE_KEY", tc.envVal)
			fileEnv := map[string]string{}
			if tc.fileVal != "" {
				fileEnv["FAKE_KEY"] = tc.fileVal
			}
			if got := BoolEnv("FAKE_KEY", fileEnv); got != tc.want {
				t.Errorf("BoolEnv(%q,%q) = %v; want %v", tc.envVal, tc.fileVal, got, tc.want)
			}
		})
	}
}

func TestLoadDotenv_MissingFileSilent(t *testing.T) {
	var buf bytes.Buffer
	got := LoadDotenv("/nonexistent/path/config.env", log.New(&buf, "", 0))
	if len(got) != 0 {
		t.Errorf("expected empty map, got %v", got)
	}
	if buf.Len() != 0 {
		t.Errorf("missing file should not log; got %q", buf.String())
	}
}

func TestLoad_OSEnvWinsPerKey(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("SIGIL_URL", "https://from-os-env.example.com")
	// Unset everything else so we get clean fallback behavior.
	for _, k := range []string{"SIGIL_USER", "SIGIL_PASSWORD", "SIGIL_CONTENT_CAPTURE_MODE", "SIGIL_EXTRA_TAGS", "SIGIL_USER_ID", "SIGIL_DEBUG", "SIGIL_OTEL_ENDPOINT"} {
		t.Setenv(k, "")
	}

	cfgDir := filepath.Join(dir, "sigil-cursor")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := `SIGIL_URL=https://from-file.example.com
SIGIL_USER=fromfile
SIGIL_PASSWORD=fromfile-pass
SIGIL_CONTENT_CAPTURE_MODE=full
`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.env"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	cfg := Load(log.New(&bytes.Buffer{}, "", 0))
	if cfg.URL != "https://from-os-env.example.com" {
		t.Errorf("URL: OS env should win; got %q", cfg.URL)
	}
	if cfg.User != "fromfile" {
		t.Errorf("User: file should fall back; got %q", cfg.User)
	}
	if cfg.Password != "fromfile-pass" {
		t.Errorf("Password: file should fall back; got %q", cfg.Password)
	}
	if cfg.ContentCapture != sigil.ContentCaptureModeFull {
		t.Errorf("ContentCapture: file value should win; got %v", cfg.ContentCapture)
	}
}

func TestHasCredentials(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		want bool
	}{
		{"all set", Config{URL: "u", User: "user", Password: "pw"}, true},
		{"missing URL", Config{User: "user", Password: "pw"}, false},
		{"missing user", Config{URL: "u", Password: "pw"}, false},
		{"missing password", Config{URL: "u", User: "user"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasCredentials(tc.cfg); got != tc.want {
				t.Errorf("HasCredentials(%+v) = %v; want %v", tc.cfg, got, tc.want)
			}
		})
	}
}
