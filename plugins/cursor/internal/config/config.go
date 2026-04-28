package config

import (
	"bufio"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/grafana/sigil-sdk/go/sigil"

	"github.com/grafana/sigil-sdk/plugins/cursor/internal/tags"
)

// Config holds all values resolved from environment + dotenv file.
type Config struct {
	URL            string
	User           string
	Password       string
	ContentCapture sigil.ContentCaptureMode
	ExtraTags      map[string]string
	UserIDOverride string
	Debug          bool
	OTel           OTelConfig
}

type OTelConfig struct {
	Endpoint string
	User     string
	Password string
	Insecure bool
}

// HasCredentials reports whether the URL/user/password are all populated.
// Without all three the plugin still runs accumulator hooks but skips Sigil
// emission.
func HasCredentials(c Config) bool {
	return c.URL != "" && c.User != "" && c.Password != ""
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

// Load resolves the runtime config by composing OS env with the dotenv file
// at FilePath(). OS env wins per-key; the file fills in unset keys.
//
// Cursor is GUI-launched, so the user's shell rc files (.zshrc/.bashrc) do
// not run for hook processes — the dotenv file is the reliable place to put
// credentials. Reading it from every hook is cheap and avoids depending on
// sessionStart firing first (which Cursor does not always guarantee).
func Load(logger *log.Logger) Config {
	fileEnv := LoadDotenv(FilePath(), logger)
	return Config{
		URL:            envOr("SIGIL_URL", fileEnv),
		User:           envOr("SIGIL_USER", fileEnv),
		Password:       envOr("SIGIL_PASSWORD", fileEnv),
		ContentCapture: resolveContentCapture(envOr("SIGIL_CONTENT_CAPTURE_MODE", fileEnv), logger),
		ExtraTags:      tags.ParseExtra(envOr("SIGIL_EXTRA_TAGS", fileEnv)),
		UserIDOverride: envOr("SIGIL_USER_ID", fileEnv),
		Debug:          BoolEnv("SIGIL_DEBUG", fileEnv),
		OTel: OTelConfig{
			Endpoint: envOr("SIGIL_OTEL_ENDPOINT", fileEnv),
			User:     envOr("SIGIL_OTEL_USER", fileEnv),
			Password: envOr("SIGIL_OTEL_PASSWORD", fileEnv),
			Insecure: BoolEnv("SIGIL_OTEL_INSECURE", fileEnv),
		},
	}
}

// envOr returns os.Getenv(key) if non-empty, else the file fallback.
func envOr(key string, fileEnv map[string]string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fileEnv[key]
}

// BoolEnv reads a boolean env var. The OS env wins; the dotenv map fills in.
// Only `true` (case-insensitive) yields true; anything else is false. The
// fileEnv arg may be nil — callers that need both sources but don't already
// have the file loaded can pass `LoadDotenv(FilePath(), nil)`.
func BoolEnv(key string, fileEnv map[string]string) bool {
	return strings.EqualFold(envOr(key, fileEnv), "true")
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
