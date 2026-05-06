package otel

import (
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

func TestInjectAuthHeaderIfMissing(t *testing.T) {
	wantBasic := "Basic " + base64.StdEncoding.EncodeToString([]byte("tenant-x:token-y"))

	cases := []struct {
		name        string
		tenant      string
		token       string
		existing    string
		want        string
		wantTouched bool
	}{
		{
			name:        "synthesizes header when none present",
			tenant:      "tenant-x",
			token:       "token-y",
			existing:    "",
			want:        "Authorization=" + wantBasic,
			wantTouched: true,
		},
		{
			name:        "appends to existing non-auth headers",
			tenant:      "tenant-x",
			token:       "token-y",
			existing:    "X-Custom=foo",
			want:        "X-Custom=foo,Authorization=" + wantBasic,
			wantTouched: true,
		},
		{
			name:        "leaves existing Authorization alone",
			tenant:      "tenant-x",
			token:       "token-y",
			existing:    "Authorization=Bearer userset",
			want:        "Authorization=Bearer userset",
			wantTouched: false,
		},
		{
			name:        "case-insensitive Authorization match",
			tenant:      "tenant-x",
			token:       "token-y",
			existing:    "authorization=Bearer userset",
			want:        "authorization=Bearer userset",
			wantTouched: false,
		},
		{
			name:        "skips when tenant missing",
			tenant:      "",
			token:       "token-y",
			existing:    "X-Custom=foo",
			want:        "X-Custom=foo",
			wantTouched: false,
		},
		{
			name:        "skips when token missing",
			tenant:      "tenant-x",
			token:       "",
			existing:    "",
			want:        "",
			wantTouched: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("SIGIL_AUTH_TENANT_ID", tc.tenant)
			t.Setenv("SIGIL_AUTH_TOKEN", tc.token)
			t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", tc.existing)

			injectAuthHeaderIfMissing()

			got := os.Getenv("OTEL_EXPORTER_OTLP_HEADERS")
			if got != tc.want {
				t.Errorf("OTEL_EXPORTER_OTLP_HEADERS = %q; want %q", got, tc.want)
			}
			if tc.wantTouched && !strings.Contains(got, "Authorization=Basic") {
				t.Errorf("expected synthesized Basic auth in headers; got %q", got)
			}
		})
	}
}

func TestHasAuthorizationHeader(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"only other header", "X-Custom=foo", false},
		{"exact case", "Authorization=Bearer x", true},
		{"lower case", "authorization=Bearer x", true},
		{"mixed case", "AuThOrIzAtIoN=Bearer x", true},
		{"trailing whitespace key", " Authorization =Bearer x", true},
		{"in middle of CSV", "X-Custom=foo,Authorization=Bearer x,X-Other=bar", true},
		{"prefix-match avoidance", "AuthorizationFake=Bearer x", false},
		{"malformed pair (no equals) ignored", "Authorization", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasAuthorizationHeader(tc.in); got != tc.want {
				t.Errorf("hasAuthorizationHeader(%q) = %v; want %v", tc.in, got, tc.want)
			}
		})
	}
}
