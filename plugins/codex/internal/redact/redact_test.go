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

func TestRedactJSONKeepsTokenCountFieldsVisible(t *testing.T) {
	raw := json.RawMessage(`{
		"max_tokens": 100,
		"input_tokens": 20,
		"output_tokens": 10,
		"total_tokens": 30,
		"promptTokenCount": 40,
		"token_count": 50,
		"tokenCount": 60,
		"tokenUsage": {"input_tokens": 20},
		"authToken": "secret",
		"idToken": "id-secret",
		"id_token": "snake-secret"
	}`)

	var got map[string]any
	if err := json.Unmarshal(New().RedactJSON(raw), &got); err != nil {
		t.Fatalf("unmarshal redacted json: %v", err)
	}
	for _, key := range []string{"max_tokens", "input_tokens", "output_tokens", "total_tokens", "promptTokenCount", "token_count", "tokenCount"} {
		if got[key] == "[REDACTED:json-secret-field]" {
			t.Fatalf("%s was over-redacted: %#v", key, got)
		}
	}
	if _, ok := got["tokenUsage"].(map[string]any); !ok {
		t.Fatalf("tokenUsage was over-redacted: %#v", got)
	}
	if got["authToken"] != "[REDACTED:json-secret-field]" {
		t.Fatalf("authToken was not redacted: %#v", got)
	}
	if got["idToken"] != "[REDACTED:json-secret-field]" {
		t.Fatalf("idToken was not redacted: %#v", got)
	}
	if got["id_token"] != "[REDACTED:json-secret-field]" {
		t.Fatalf("id_token was not redacted: %#v", got)
	}
}

func TestRedactJSONRedactsServiceTokenKeys(t *testing.T) {
	raw := json.RawMessage(`{
		"github_token": "short-github-token",
		"grafanaToken": "short-grafana-token",
		"slack_token": "short-slack-token",
		"serviceToken": "short-service-token"
	}`)

	var got map[string]any
	if err := json.Unmarshal(New().RedactJSON(raw), &got); err != nil {
		t.Fatalf("unmarshal redacted json: %v", err)
	}
	for _, key := range []string{"github_token", "grafanaToken", "slack_token", "serviceToken"} {
		if got[key] != "[REDACTED:json-secret-field]" {
			t.Fatalf("%s was not redacted: %#v", key, got)
		}
	}
}
