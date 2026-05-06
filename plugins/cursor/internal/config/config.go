// Package config resolves the cursor plugin's runtime knobs from OS env and a
// dotenv file at $XDG_CONFIG_HOME/sigil-cursor/config.env.
//
// Cursor launches hooks under a stripped environment (shell rc files do not
// run), so the dotenv is the reliable place to put credentials. ApplyEnv
// copies dotenv values into the process env so the SDK's own canonical
// SIGIL_* env-var resolution (see go/sigil/env.go) sees them too.
package config

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/grafana/sigil-sdk/go/sigil"
)

// Config holds the cursor-side knobs the plugin needs before constructing the
// SDK client. Endpoint, auth, and SIGIL_TAGS are read by the SDK directly via
// resolveFromEnv — they are deliberately absent here. OTel transport
// (OTEL_EXPORTER_OTLP_*) is read by the OpenTelemetry SDK exporters
// natively, so this struct doesn't carry it either.
type Config struct {
	ContentCapture sigil.ContentCaptureMode
	UserIDOverride string
	Debug          bool
}

// HasCredentials reports whether the canonical SIGIL_* credentials are
// populated in the OS env. Call after ApplyEnv so dotenv-supplied values are
// visible.
func HasCredentials() bool {
	return os.Getenv("SIGIL_ENDPOINT") != "" &&
		os.Getenv("SIGIL_AUTH_TENANT_ID") != "" &&
		os.Getenv("SIGIL_AUTH_TOKEN") != ""
}

// FilePath is the dotenv config path. Honors XDG_CONFIG_HOME, falls back to
// $HOME/.config/sigil-cursor/config.env.
func FilePath() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "sigil-cursor", "config.env")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "sigil-cursor", "config.env")
}

// ApplyEnv loads the dotenv file and writes any keys whose OS env value is
// empty. Idempotent — a non-empty OS value always wins per-key. Returns the
// parsed dotenv map for callers that need to introspect (currently only
// tests).
//
// Calling this once at process start is what makes the SDK's automatic
// SIGIL_* resolution work under Cursor's stripped hook environment. An
// empty-but-set OS value is treated as unset to match the SDK's own
// envTrimmed behaviour and to handle hosts that pre-create blank vars.
func ApplyEnv(logger *log.Logger) map[string]string {
	fileEnv := LoadDotenv(FilePath(), logger)
	for k, v := range fileEnv {
		if existing := os.Getenv(k); existing != "" {
			continue
		}
		_ = os.Setenv(k, v)
	}
	return fileEnv
}

// Load resolves the cursor-local subset of config from OS env. Call ApplyEnv
// first so dotenv-only values are reflected in the OS env.
func Load(logger *log.Logger) Config {
	return Config{
		ContentCapture: resolveContentCapture(os.Getenv("SIGIL_CONTENT_CAPTURE_MODE"), logger),
		UserIDOverride: os.Getenv("SIGIL_USER_ID"),
		Debug:          parseBool(os.Getenv("SIGIL_DEBUG")),
	}
}

// parseBool mirrors the SDK's parseBool whitelist (1/true/yes/on). Anything
// else (including the empty string) is false.
func parseBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// resolveContentCapture parses SIGIL_CONTENT_CAPTURE_MODE. Unknown values
// fail closed to MetadataOnly with a warning log so a typo doesn't silently
// downgrade behaviour.
func resolveContentCapture(raw string, logger *log.Logger) sigil.ContentCaptureMode {
	if raw == "" {
		return sigil.ContentCaptureModeMetadataOnly
	}
	var mode sigil.ContentCaptureMode
	if err := mode.UnmarshalText([]byte(raw)); err != nil {
		if logger != nil {
			logger.Printf("config: unknown SIGIL_CONTENT_CAPTURE_MODE=%q — falling back to metadata_only", raw)
		}
		return sigil.ContentCaptureModeMetadataOnly
	}
	if mode == sigil.ContentCaptureModeDefault {
		return sigil.ContentCaptureModeMetadataOnly
	}
	return mode
}

// parseDotenvValue strips surrounding quotes (if both ends match) and a
// trailing ` # comment` from an unquoted value. Quoted values keep their
// inner whitespace and any `#` characters verbatim — including on a quoted
// value followed by an inline comment like `KEY="x" # comment`.
func parseDotenvValue(v string) string {
	if len(v) >= 2 {
		first := v[0]
		if first == '"' || first == '\'' {
			if end := strings.IndexByte(v[1:], first); end >= 0 {
				return v[1 : 1+end]
			}
			// Unterminated quote — fall through and treat as raw.
		}
	}
	if hashIdx := strings.Index(v, " #"); hashIdx >= 0 {
		v = strings.TrimRight(v[:hashIdx], " \t")
	}
	return v
}

// LoadDotenv parses the file at path. Missing files return an empty map
// silently; other errors are logged. Format:
//   - `KEY=value` one pair per line
//   - `# comment` lines and trailing ` # comment` on unquoted values
//   - optional single- or double-quoted values
//   - optional leading `export ` prefix
func LoadDotenv(path string, logger *log.Logger) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) && logger != nil {
			logger.Printf("config: read %s: %v", path, err)
		}
		return out
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(line[len("export "):])
		}
		key, rest, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		value := parseDotenvValue(strings.TrimSpace(rest))
		if value != "" {
			out[key] = value
		}
	}
	if err := scanner.Err(); err != nil && logger != nil {
		logger.Printf("config: scan %s: %v", path, err)
	}
	return out
}
