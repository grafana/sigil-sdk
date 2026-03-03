package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRejectsZeroTimeout(t *testing.T) {
	testCases := []struct {
		name    string
		timeout string
	}{
		{name: "zero timeout", timeout: "0s"},
		{name: "negative timeout", timeout: "-1s"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			var stdout bytes.Buffer
			var stderr bytes.Buffer

			err := run(
				[]string{"-endpoint", "localhost:4317", "-token", "tok", "-timeout", testCase.timeout},
				&stdout,
				&stderr,
			)
			if err == nil {
				t.Fatal("expected timeout validation error, got nil")
			}
			if !strings.Contains(err.Error(), "timeout must be > 0") {
				t.Fatalf("expected timeout validation error, got %v", err)
			}
		})
	}
}

func TestReadDotEnvValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "" +
		"# ignored\n" +
		"PLAIN=value-a\n" +
		"QUOTED=\"value b\"\n" +
		"EXPORTED='value-c'\n" +
		"export TOKEN=token-from-dotenv # trailing\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	testCases := []struct {
		name string
		key  string
		want string
	}{
		{name: "plain", key: "PLAIN", want: "value-a"},
		{name: "double quoted", key: "QUOTED", want: "value b"},
		{name: "single quoted", key: "EXPORTED", want: "value-c"},
		{name: "export syntax with comment", key: "TOKEN", want: "token-from-dotenv"},
		{name: "missing", key: "UNKNOWN", want: ""},
	}

	for _, testCase := range testCases {
		got, err := readDotEnvValue(path, testCase.key)
		if err != nil {
			t.Fatalf("%s: readDotEnvValue returned error: %v", testCase.name, err)
		}
		if got != testCase.want {
			t.Fatalf("%s: expected %q, got %q", testCase.name, testCase.want, got)
		}
	}
}

func TestResolveTokenPriority(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	const tokenEnv = "SIGIL_TEST_TOKEN"
	if err := os.WriteFile(path, []byte(tokenEnv+"=dotenv-token\n"), 0o644); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	t.Setenv(tokenEnv, "")

	token, source, err := resolveToken("flag-token", tokenEnv, path)
	if err != nil {
		t.Fatalf("resolve with flag token returned error: %v", err)
	}
	if token != "flag-token" || source != "flag:-token" {
		t.Fatalf("expected flag token precedence, got token=%q source=%q", token, source)
	}

	t.Setenv(tokenEnv, "env-token")
	token, source, err = resolveToken("", tokenEnv, path)
	if err != nil {
		t.Fatalf("resolve with env token returned error: %v", err)
	}
	if token != "env-token" || source != "env:"+tokenEnv {
		t.Fatalf("expected env token precedence, got token=%q source=%q", token, source)
	}

	t.Setenv(tokenEnv, "")
	token, source, err = resolveToken("", tokenEnv, path)
	if err != nil {
		t.Fatalf("resolve with dotenv token returned error: %v", err)
	}
	if token != "dotenv-token" || source != "dotenv:"+path {
		t.Fatalf("expected dotenv token fallback, got token=%q source=%q", token, source)
	}
}

func TestFindRejectionForGeneration(t *testing.T) {
	logOutput := "" +
		"2026/03/02 10:00:00 sigil generation rejected id=gen-a error=invalid tenant\n" +
		"2026/03/02 10:00:01 sigil generation rejected id=gen-b error=missing input\n"

	if got := findRejectionForGeneration(logOutput, "gen-a"); got != "invalid tenant" {
		t.Fatalf("expected invalid tenant, got %q", got)
	}
	if got := findRejectionForGeneration(logOutput, "gen-b"); got != "missing input" {
		t.Fatalf("expected missing input, got %q", got)
	}
	if got := findRejectionForGeneration(logOutput, "gen-c"); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestFormatBasicAuth(t *testing.T) {
	got := formatBasicAuth("4130", "abc123")
	if got != "Basic NDEzMDphYmMxMjM=" {
		t.Fatalf("unexpected basic auth header: %q", got)
	}
}

func TestResolveReadBaseURL(t *testing.T) {
	testCases := []struct {
		name        string
		endpoint    string
		readBaseURL string
		insecure    bool
		want        string
		expectErr   bool
	}{
		{
			name:     "derive from tls endpoint",
			endpoint: "sigil-dev-001.grafana-dev.net:443",
			insecure: false,
			want:     "https://sigil-dev-001.grafana-dev.net",
		},
		{
			name:     "derive from insecure endpoint",
			endpoint: "localhost:4317",
			insecure: true,
			want:     "http://localhost:4317",
		},
		{
			name:        "explicit base url",
			endpoint:    "localhost:4317",
			readBaseURL: "https://example.com/",
			insecure:    false,
			want:        "https://example.com",
		},
		{
			name:        "invalid explicit url",
			endpoint:    "localhost:4317",
			readBaseURL: "example.com",
			expectErr:   true,
		},
		{
			name:     "http scheme endpoint without insecure flag",
			endpoint: "http://localhost:4317",
			insecure: false,
			want:     "http://localhost:4317",
		},
		{
			name:     "https scheme endpoint with insecure flag",
			endpoint: "https://sigil.example.com:443",
			insecure: true,
			want:     "http://sigil.example.com",
		},
	}

	for _, testCase := range testCases {
		got, err := resolveReadBaseURL(testCase.endpoint, testCase.readBaseURL, testCase.insecure)
		if testCase.expectErr {
			if err == nil {
				t.Fatalf("%s: expected error, got nil", testCase.name)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", testCase.name, err)
		}
		if got != testCase.want {
			t.Fatalf("%s: expected %q, got %q", testCase.name, testCase.want, got)
		}
	}
}
