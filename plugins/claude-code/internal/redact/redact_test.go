package redact

import (
	"strings"
	"testing"
)

func TestRedactor_Tier1Patterns(t *testing.T) {
	r := New()

	tests := []struct {
		name    string
		input   string
		wantID  string
		wantHit bool
	}{
		{"grafana cloud token", "token is glc_abcdefghijklmnopqrstuvwx", "grafana-cloud-token", true},
		{"grafana SA token", "use glsa_abcdefghijklmnopqrstuvwx", "grafana-service-account-token", true},
		{"AWS AKIA key", "AKIAIOSFODNN7EXAMPLE", "aws-access-token", true},
		{"AWS ASIA key", "ASIAIOSFODNN7EXAMPLE", "aws-access-token", true},
		{"GitHub PAT", "ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij", "github-pat", true},
		{"GitHub OAuth", "gho_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij", "github-oauth", true},
		{"GitHub App token", "ghs_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij", "github-app-token", true},
		{"OpenAI project key", "sk-proj-abcdefghijklmnopqrstuvwxyz1234567890abcd", "openai-project-key", true},
		{"GCP API key", "AIzaSyA1234567890abcdefghijklmnopqrstuvw", "gcp-api-key", true},
		{"Bearer token", "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.abc", "bearer-token", true},
		{"Slack token", "xoxb-1234567890-abcdefghij", "slack-token", true},
		{"Stripe test key", "sk_test_abcdefghijklmnopqrstuvwxyz", "stripe-key", true},
		{"SendGrid key", "SG.abcdefghijklmnopqrstuv.abcdefghijklmnopqrstuvwxyz12345678901234567", "sendgrid-api-key", true},
		{"npm token", "npm_abcdefghijklmnopqrstuvwxyz1234567890", "npm-token", true},
		{"connection string", "postgres://user:pass@localhost:5432/db", "connection-string", true},
		{"PEM private key", "-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----", "private-key", true},

		// Negative cases
		{"normal text", "hello world", "", false},
		{"short token-like", "glc_short", "", false}, // less than 20 chars
		{"code variable", "const myKey = getValue()", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.Redact(tt.input)
			hasRedaction := strings.Contains(result, "[REDACTED:")

			if hasRedaction != tt.wantHit {
				t.Errorf("Redact(%q) redacted=%v, want=%v\nresult: %s", tt.input, hasRedaction, tt.wantHit, result)
			}

			if tt.wantHit && !strings.Contains(result, "[REDACTED:"+tt.wantID+"]") {
				t.Errorf("expected pattern ID %q in result: %s", tt.wantID, result)
			}
		})
	}
}

func TestRedactor_Tier2Patterns(t *testing.T) {
	r := New()

	tests := []struct {
		name    string
		input   string
		wantHit bool
	}{
		{"env PASSWORD", "DATABASE_PASSWORD=mysecretpass123", true},
		{"env SECRET", "APP_SECRET: s3cr3t_v4lu3", true},
		{"env API_KEY", "API_KEY=abcdef12345", true},
		{"json password field", `"password": "hunter2"`, true},
		{"json secret field", `"client_secret": "abcdef123456"`, true},
		{"normal assignment", "count = 42", false},
		{"normal json", `"name": "alice"`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := r.Redact(tt.input)
			hasRedaction := strings.Contains(result, "[REDACTED:")

			if hasRedaction != tt.wantHit {
				t.Errorf("Redact(%q) redacted=%v, want=%v\nresult: %s", tt.input, hasRedaction, tt.wantHit, result)
			}
		})
	}
}

func TestRedactor_LightweightSkipsTier2(t *testing.T) {
	r := New()

	// Tier 2 pattern should NOT be caught by lightweight
	input := "DATABASE_PASSWORD=mysecretpass123"
	result := r.RedactLightweight(input)
	if strings.Contains(result, "[REDACTED:") {
		t.Errorf("RedactLightweight should not catch Tier 2 patterns, got: %s", result)
	}

	// Tier 1 pattern should still be caught
	input2 := "token glc_abcdefghijklmnopqrstuvwx"
	result2 := r.RedactLightweight(input2)
	if !strings.Contains(result2, "[REDACTED:grafana-cloud-token]") {
		t.Errorf("RedactLightweight should catch Tier 1 patterns, got: %s", result2)
	}
}

func TestRedactor_MultipleSecrets(t *testing.T) {
	r := New()

	input := "use glc_abcdefghijklmnopqrstuvwx and Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.abc"
	result := r.Redact(input)

	if !strings.Contains(result, "[REDACTED:grafana-cloud-token]") {
		t.Error("missing grafana-cloud-token redaction")
	}
	if !strings.Contains(result, "[REDACTED:bearer-token]") {
		t.Error("missing bearer-token redaction")
	}
}

func TestRedactor_NoFalsePositivesOnCode(t *testing.T) {
	r := New()

	codeSnippets := []string{
		`func getAPIKey() string { return os.Getenv("API_KEY") }`,
		`if err != nil { return fmt.Errorf("bearer auth failed: %w", err) }`,
		`const maxTokens = 4096`,
		`log.Info("processing request", "token_count", len(tokens))`,
		`var sk = newSecretKeeper()`,
	}

	for _, code := range codeSnippets {
		result := r.RedactLightweight(code)
		if strings.Contains(result, "[REDACTED:") {
			t.Errorf("false positive on code: %q -> %q", code, result)
		}
	}
}
