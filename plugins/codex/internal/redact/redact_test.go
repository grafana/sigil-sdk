package redact

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactCoversRepoStandardTokenCorpus(t *testing.T) {
	cases := map[string]string{
		"anthropic-admin-key": "sk-ant-admin01-" + strings.Repeat("A", 93) + "AA",
		"openai-api-key":      "sk-" + strings.Repeat("A", 20) + "T3BlbkFJ" + strings.Repeat("B", 20),
		"sendgrid-api-key":    "SG." + strings.Repeat("A", 22) + "." + strings.Repeat("B", 43),
		"twilio-api-key":      "SK" + strings.Repeat("a", 32),
		"npm-token":           "npm_" + strings.Repeat("A", 36),
		"pypi-token":          "pypi-" + strings.Repeat("A", 50),
	}
	red := New()
	for name, secret := range cases {
		t.Run(name, func(t *testing.T) {
			got := red.Redact("secret=" + secret)
			if strings.Contains(got, secret) {
				t.Fatalf("secret leaked: %s", got)
			}
			if !strings.Contains(got, "[REDACTED:") {
				t.Fatalf("missing redaction marker for %s: %s", name, got)
			}
		})
	}
}

func TestRedactJSONCoversRepoStandardTokenCorpus(t *testing.T) {
	secret := "SG." + strings.Repeat("A", 22) + "." + strings.Repeat("B", 43)
	raw := json.RawMessage(`{"value":"` + secret + `"}`)

	got := string(New().RedactJSON(raw))
	if strings.Contains(got, secret) {
		t.Fatalf("secret leaked: %s", got)
	}
	if !strings.Contains(got, "[REDACTED:sendgrid-api-key]") {
		t.Fatalf("missing sendgrid marker: %s", got)
	}
}
