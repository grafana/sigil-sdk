package sigil

import (
	"context"
	"testing"
)

func mapLookup(env map[string]string) envLookup {
	return func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	}
}

// TestResolveFromEnv covers the env-only resolution layer: every SIGIL_*
// variable, malformed-input handling, and the partial-config contract that
// invalid fields are skipped without dropping the valid ones.
func TestResolveFromEnv(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		wantErr bool
		check   func(t *testing.T, cfg Config)
	}{
		{
			name: "no env uses defaults",
			env:  map[string]string{},
			check: func(t *testing.T, cfg Config) {
				if cfg.GenerationExport.Endpoint != "localhost:4317" {
					t.Errorf("Endpoint=%q want localhost:4317", cfg.GenerationExport.Endpoint)
				}
				if cfg.GenerationExport.Protocol != GenerationExportProtocolGRPC {
					t.Errorf("Protocol=%q want grpc", cfg.GenerationExport.Protocol)
				}
			},
		},
		{
			name: "transport from env",
			env: map[string]string{
				"SIGIL_ENDPOINT": "https://env:4318",
				"SIGIL_PROTOCOL": "http",
				"SIGIL_INSECURE": "true",
				"SIGIL_HEADERS":  "X-A=1,X-B=two",
			},
			check: func(t *testing.T, cfg Config) {
				if cfg.GenerationExport.Endpoint != "https://env:4318" {
					t.Errorf("Endpoint=%q", cfg.GenerationExport.Endpoint)
				}
				if cfg.GenerationExport.Protocol != GenerationExportProtocolHTTP {
					t.Errorf("Protocol=%q", cfg.GenerationExport.Protocol)
				}
				if cfg.GenerationExport.Insecure == nil || !*cfg.GenerationExport.Insecure {
					t.Errorf("Insecure=%v want true", cfg.GenerationExport.Insecure)
				}
				if cfg.GenerationExport.Headers["X-A"] != "1" || cfg.GenerationExport.Headers["X-B"] != "two" {
					t.Errorf("Headers=%v", cfg.GenerationExport.Headers)
				}
			},
		},
		{
			name: "basic auth from env",
			env: map[string]string{
				"SIGIL_AUTH_MODE":      "basic",
				"SIGIL_AUTH_TENANT_ID": "42",
				"SIGIL_AUTH_TOKEN":     "glc_xxx",
			},
			check: func(t *testing.T, cfg Config) {
				auth := cfg.GenerationExport.Auth
				if auth.Mode != ExportAuthModeBasic {
					t.Errorf("Mode=%q", auth.Mode)
				}
				if auth.TenantID != "42" {
					t.Errorf("TenantID=%q", auth.TenantID)
				}
				if auth.BasicPassword != "glc_xxx" {
					t.Errorf("BasicPassword=%q", auth.BasicPassword)
				}
			},
		},
		{
			name: "bearer auth from env",
			env: map[string]string{
				"SIGIL_AUTH_MODE":  "bearer",
				"SIGIL_AUTH_TOKEN": "tok",
			},
			check: func(t *testing.T, cfg Config) {
				auth := cfg.GenerationExport.Auth
				if auth.Mode != ExportAuthModeBearer {
					t.Errorf("Mode=%q", auth.Mode)
				}
				if auth.BearerToken != "tok" {
					t.Errorf("BearerToken=%q", auth.BearerToken)
				}
			},
		},
		{
			name:    "invalid auth mode returns error",
			env:     map[string]string{"SIGIL_AUTH_MODE": "garbage"},
			wantErr: true,
		},
		{
			name: "invalid auth mode preserves other valid env",
			env: map[string]string{
				"SIGIL_AUTH_MODE":  "Bearrer",
				"SIGIL_ENDPOINT":   "valid.example:4318",
				"SIGIL_AGENT_NAME": "valid-agent",
				"SIGIL_USER_ID":    "alice",
			},
			wantErr: true,
			check: func(t *testing.T, cfg Config) {
				if cfg.GenerationExport.Endpoint != "valid.example:4318" {
					t.Errorf("Endpoint=%q want valid.example:4318 (preserved despite auth-mode typo)", cfg.GenerationExport.Endpoint)
				}
				if cfg.AgentName != "valid-agent" {
					t.Errorf("AgentName=%q (preserved despite auth-mode typo)", cfg.AgentName)
				}
				if cfg.UserID != "alice" {
					t.Errorf("UserID=%q (preserved despite auth-mode typo)", cfg.UserID)
				}
			},
		},
		{
			// resolveHeadersWithAuth ignores TenantID for mode=none, so the
			// stray var is harmless. See TestNewClient_EnvHandling for the
			// end-to-end "doesn't panic" guarantee.
			name: "stray SIGIL_AUTH_TENANT_ID keeps env mode at none",
			env: map[string]string{
				"SIGIL_AUTH_TENANT_ID": "42",
			},
			check: func(t *testing.T, cfg Config) {
				if cfg.GenerationExport.Auth.Mode != ExportAuthModeNone {
					t.Errorf("Mode=%q want none (env did not override)", cfg.GenerationExport.Auth.Mode)
				}
			},
		},
		{
			name: "agent / user / tags / debug from env",
			env: map[string]string{
				"SIGIL_AGENT_NAME":    "planner",
				"SIGIL_AGENT_VERSION": "1.2.3",
				"SIGIL_USER_ID":       "alice@example.com",
				"SIGIL_TAGS":          "service=orchestrator,env=prod",
				"SIGIL_DEBUG":         "true",
			},
			check: func(t *testing.T, cfg Config) {
				if cfg.AgentName != "planner" {
					t.Errorf("AgentName=%q", cfg.AgentName)
				}
				if cfg.AgentVersion != "1.2.3" {
					t.Errorf("AgentVersion=%q", cfg.AgentVersion)
				}
				if cfg.UserID != "alice@example.com" {
					t.Errorf("UserID=%q", cfg.UserID)
				}
				if cfg.Tags["service"] != "orchestrator" || cfg.Tags["env"] != "prod" {
					t.Errorf("Tags=%v", cfg.Tags)
				}
				if cfg.Debug == nil || !*cfg.Debug {
					t.Errorf("Debug=%v want true", cfg.Debug)
				}
			},
		},
		{
			name: "content capture mode from env",
			env:  map[string]string{"SIGIL_CONTENT_CAPTURE_MODE": "metadata_only"},
			check: func(t *testing.T, cfg Config) {
				if cfg.ContentCapture != ContentCaptureModeMetadataOnly {
					t.Errorf("ContentCapture=%v", cfg.ContentCapture)
				}
			},
		},
		{
			name:    "invalid content capture mode returns error",
			env:     map[string]string{"SIGIL_CONTENT_CAPTURE_MODE": "bogus"},
			wantErr: true,
		},
		{
			name: "invalid content capture mode preserves other valid env",
			env: map[string]string{
				"SIGIL_CONTENT_CAPTURE_MODE": "bogus",
				"SIGIL_ENDPOINT":             "valid.example:4318",
				"SIGIL_AGENT_NAME":           "valid-agent",
			},
			wantErr: true,
			check: func(t *testing.T, cfg Config) {
				if cfg.GenerationExport.Endpoint != "valid.example:4318" {
					t.Errorf("Endpoint=%q (preserved despite content-capture typo)", cfg.GenerationExport.Endpoint)
				}
				if cfg.AgentName != "valid-agent" {
					t.Errorf("AgentName=%q (preserved despite content-capture typo)", cfg.AgentName)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := resolveFromEnv(mapLookup(tc.env), DefaultConfig())
			if tc.wantErr && err == nil {
				t.Fatalf("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, cfg)
			}
		})
	}
}

func TestParseCSVKV(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want map[string]string
	}{
		{
			name: "mixed valid and edge cases",
			raw:  "a=1, b = two ,, =skip,c=",
			want: map[string]string{"a": "1", "b": "two", "c": ""},
		},
		{
			name: "empty input",
			raw:  "",
			want: map[string]string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseCSVKV(tc.raw)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("got[%q]=%q want %q", k, got[k], v)
				}
			}
		})
	}
}

// TestNewClient_EnvHandling exercises the integration of env resolution with
// caller-supplied Config: precedence rules, malformed-env recovery, and the
// auth mode/credential interaction.
func TestNewClient_EnvHandling(t *testing.T) {
	cases := []struct {
		name  string
		env   map[string]string
		cfg   Config
		check func(t *testing.T, c *Client)
	}{
		{
			name: "reads env into empty config",
			env: map[string]string{
				"SIGIL_AGENT_NAME": "from-env",
				"SIGIL_USER_ID":    "alice",
				"SIGIL_TAGS":       "team=ai",
				"SIGIL_PROTOCOL":   "none",
			},
			check: func(t *testing.T, c *Client) {
				if c.config.AgentName != "from-env" {
					t.Errorf("AgentName=%q", c.config.AgentName)
				}
				if c.config.UserID != "alice" {
					t.Errorf("UserID=%q", c.config.UserID)
				}
				if c.config.Tags["team"] != "ai" {
					t.Errorf("Tags=%v", c.config.Tags)
				}
				if c.config.GenerationExport.Protocol != GenerationExportProtocolNone {
					t.Errorf("Protocol=%v", c.config.GenerationExport.Protocol)
				}
			},
		},
		{
			name: "explicit caller value wins over env",
			env:  map[string]string{"SIGIL_ENDPOINT": "env-endpoint:4318"},
			cfg: Config{
				GenerationExport: GenerationExportConfig{
					Endpoint: "explicit-endpoint:4318",
					Protocol: GenerationExportProtocolNone,
				},
			},
			check: func(t *testing.T, c *Client) {
				if c.config.GenerationExport.Endpoint != "explicit-endpoint:4318" {
					t.Errorf("Endpoint=%q", c.config.GenerationExport.Endpoint)
				}
			},
		},
		{
			name: "env Insecure=true survives empty caller config",
			env: map[string]string{
				"SIGIL_INSECURE": "true",
				"SIGIL_PROTOCOL": "none",
			},
			check: func(t *testing.T, c *Client) {
				if c.config.GenerationExport.Insecure == nil || !*c.config.GenerationExport.Insecure {
					t.Fatalf("Insecure=%v, want env-resolved true", c.config.GenerationExport.Insecure)
				}
			},
		},
		{
			name: "explicit Insecure=false beats env Insecure=true",
			env: map[string]string{
				"SIGIL_INSECURE": "true",
				"SIGIL_PROTOCOL": "none",
			},
			cfg: Config{GenerationExport: GenerationExportConfig{Insecure: BoolPtr(false)}},
			check: func(t *testing.T, c *Client) {
				if c.config.GenerationExport.Insecure == nil || *c.config.GenerationExport.Insecure {
					t.Fatalf("Insecure=%v, want explicit false", c.config.GenerationExport.Insecure)
				}
			},
		},
		{
			name: "env Debug=true survives empty caller config",
			env: map[string]string{
				"SIGIL_DEBUG":    "true",
				"SIGIL_PROTOCOL": "none",
			},
			check: func(t *testing.T, c *Client) {
				if c.config.Debug == nil || !*c.config.Debug {
					t.Fatalf("Debug=%v, want env-resolved true", c.config.Debug)
				}
			},
		},
		{
			name: "explicit Debug=false beats env Debug=true",
			env: map[string]string{
				"SIGIL_DEBUG":    "true",
				"SIGIL_PROTOCOL": "none",
			},
			cfg: Config{Debug: BoolPtr(false)},
			check: func(t *testing.T, c *Client) {
				if c.config.Debug == nil || *c.config.Debug {
					t.Fatalf("Debug=%v, want explicit false", c.config.Debug)
				}
			},
		},
		{
			name: "malformed SIGIL_AUTH_MODE does not panic",
			env: map[string]string{
				"SIGIL_AUTH_MODE": "Bearrer",
				"SIGIL_PROTOCOL":  "none",
			},
		},
		{
			name: "malformed SIGIL_AUTH_MODE preserves valid env siblings",
			env: map[string]string{
				"SIGIL_AUTH_MODE":  "Bearrer",
				"SIGIL_ENDPOINT":   "valid.example:4318",
				"SIGIL_AGENT_NAME": "valid-agent",
				"SIGIL_USER_ID":    "alice",
				"SIGIL_PROTOCOL":   "none",
			},
			check: func(t *testing.T, c *Client) {
				if c.config.GenerationExport.Endpoint != "valid.example:4318" {
					t.Errorf("Endpoint=%q want valid.example:4318 (preserved despite typo)", c.config.GenerationExport.Endpoint)
				}
				if c.config.AgentName != "valid-agent" {
					t.Errorf("AgentName=%q (preserved despite typo)", c.config.AgentName)
				}
				if c.config.UserID != "alice" {
					t.Errorf("UserID=%q (preserved despite typo)", c.config.UserID)
				}
			},
		},
		{
			name: "stray SIGIL_AUTH_TENANT_ID does not panic",
			env: map[string]string{
				"SIGIL_AUTH_TENANT_ID": "42",
				"SIGIL_PROTOCOL":       "none",
			},
			check: func(t *testing.T, c *Client) {
				if c.config.GenerationExport.Auth.Mode != ExportAuthModeNone {
					t.Errorf("Mode=%q want none", c.config.GenerationExport.Auth.Mode)
				}
			},
		},
		{
			name: "caller bearer mode wins over env basic mode",
			env: map[string]string{
				"SIGIL_AUTH_MODE":      "basic",
				"SIGIL_AUTH_TENANT_ID": "42",
				"SIGIL_AUTH_TOKEN":     "envpass",
				"SIGIL_PROTOCOL":       "none",
			},
			cfg: Config{
				GenerationExport: GenerationExportConfig{
					Auth: AuthConfig{Mode: ExportAuthModeBearer, BearerToken: "callertok"},
				},
			},
			check: func(t *testing.T, c *Client) {
				auth := c.config.GenerationExport.Auth
				if auth.Mode != ExportAuthModeBearer {
					t.Errorf("Mode=%q want bearer (caller wins)", auth.Mode)
				}
				if auth.BearerToken != "callertok" {
					t.Errorf("BearerToken=%q want callertok", auth.BearerToken)
				}
				// Authorization header carries caller's bearer token, not env's password.
				got := c.config.GenerationExport.Headers["Authorization"]
				if got != "Bearer callertok" {
					t.Errorf("Authorization=%q want %q", got, "Bearer callertok")
				}
			},
		},
		{
			// Caller tags merge with env tags as a base layer; caller wins on
			// key collision. Matches JS and Python SDK behavior.
			name: "caller tags merge with env tags",
			env: map[string]string{
				"SIGIL_TAGS":     "service=orch,env=prod",
				"SIGIL_PROTOCOL": "none",
			},
			cfg: Config{
				Tags: map[string]string{"team": "ai", "env": "staging"},
			},
			check: func(t *testing.T, c *Client) {
				if got := c.config.Tags["service"]; got != "orch" {
					t.Errorf("Tags[service]=%q want orch (env-filled)", got)
				}
				if got := c.config.Tags["team"]; got != "ai" {
					t.Errorf("Tags[team]=%q want ai (caller-only)", got)
				}
				if got := c.config.Tags["env"]; got != "staging" {
					t.Errorf("Tags[env]=%q want staging (caller wins on collision)", got)
				}
			},
		},
		{
			name: "env SIGIL_AUTH_TOKEN fills caller-supplied bearer mode",
			env: map[string]string{
				"SIGIL_AUTH_TOKEN": "envtok",
				"SIGIL_PROTOCOL":   "none",
			},
			cfg: Config{
				GenerationExport: GenerationExportConfig{
					Auth: AuthConfig{Mode: ExportAuthModeBearer},
				},
			},
			check: func(t *testing.T, c *Client) {
				auth := c.config.GenerationExport.Auth
				if auth.Mode != ExportAuthModeBearer {
					t.Errorf("Mode=%q want bearer", auth.Mode)
				}
				if auth.BearerToken != "envtok" {
					t.Errorf("BearerToken=%q want envtok (filled from SIGIL_AUTH_TOKEN)", auth.BearerToken)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("NewClient panicked: %v", r)
				}
			}()
			c := NewClient(tc.cfg)
			defer func() { _ = c.Shutdown(context.Background()) }()
			if tc.check != nil {
				tc.check(t, c)
			}
		})
	}
}
