package sigil

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace/noop"
)

func TestResolveHeadersWithAuthTenantMode(t *testing.T) {
	headers, err := resolveHeadersWithAuth(nil, AuthConfig{Mode: ExportAuthModeTenant, TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("resolve headers: %v", err)
	}
	if headers[tenantHeaderName] != "tenant-a" {
		t.Fatalf("expected tenant header tenant-a, got %q", headers[tenantHeaderName])
	}
}

func TestResolveHeadersWithAuthBearerMode(t *testing.T) {
	headers, err := resolveHeadersWithAuth(nil, AuthConfig{Mode: ExportAuthModeBearer, BearerToken: "token-123"})
	if err != nil {
		t.Fatalf("resolve headers: %v", err)
	}
	if headers[authorizationHeaderName] != "Bearer token-123" {
		t.Fatalf("expected bearer header, got %q", headers[authorizationHeaderName])
	}
}

func TestResolveHeadersWithAuthExplicitHeaderWins(t *testing.T) {
	headers, err := resolveHeadersWithAuth(map[string]string{
		"x-scope-orgid": "tenant-override",
		"authorization": "Bearer override-token",
	}, AuthConfig{Mode: ExportAuthModeTenant, TenantID: "tenant-a"})
	if err != nil {
		t.Fatalf("resolve tenant headers: %v", err)
	}
	if headers["x-scope-orgid"] != "tenant-override" {
		t.Fatalf("expected override tenant header, got %q", headers["x-scope-orgid"])
	}

	bearerHeaders, err := resolveHeadersWithAuth(map[string]string{
		"authorization": "Bearer override-token",
	}, AuthConfig{Mode: ExportAuthModeBearer, BearerToken: "token-123"})
	if err != nil {
		t.Fatalf("resolve bearer headers: %v", err)
	}
	if bearerHeaders["authorization"] != "Bearer override-token" {
		t.Fatalf("expected override authorization header, got %q", bearerHeaders["authorization"])
	}
}

func TestResolveHeadersWithAuthBasicMode(t *testing.T) {
	headers, err := resolveHeadersWithAuth(nil, AuthConfig{
		Mode:          ExportAuthModeBasic,
		TenantID:      "42",
		BasicPassword: "secret",
	})
	if err != nil {
		t.Fatalf("resolve headers: %v", err)
	}
	wantAuth := "Basic " + base64Encode("42:secret")
	if headers[authorizationHeaderName] != wantAuth {
		t.Fatalf("expected %q, got %q", wantAuth, headers[authorizationHeaderName])
	}
	if headers[tenantHeaderName] != "42" {
		t.Fatalf("expected tenant header 42, got %q", headers[tenantHeaderName])
	}
}

func TestResolveHeadersWithAuthBasicModeExplicitUser(t *testing.T) {
	headers, err := resolveHeadersWithAuth(nil, AuthConfig{
		Mode:          ExportAuthModeBasic,
		TenantID:      "42",
		BasicUser:     "probe-user",
		BasicPassword: "secret",
	})
	if err != nil {
		t.Fatalf("resolve headers: %v", err)
	}
	wantAuth := "Basic " + base64Encode("probe-user:secret")
	if headers[authorizationHeaderName] != wantAuth {
		t.Fatalf("expected %q, got %q", wantAuth, headers[authorizationHeaderName])
	}
	if headers[tenantHeaderName] != "42" {
		t.Fatalf("expected tenant header 42, got %q", headers[tenantHeaderName])
	}
}

func TestResolveHeadersWithAuthBasicModeExplicitHeaderWins(t *testing.T) {
	headers, err := resolveHeadersWithAuth(map[string]string{
		"Authorization": "Basic override",
		"X-Scope-OrgID": "override-tenant",
	}, AuthConfig{
		Mode:          ExportAuthModeBasic,
		TenantID:      "42",
		BasicPassword: "secret",
	})
	if err != nil {
		t.Fatalf("resolve headers: %v", err)
	}
	if headers["Authorization"] != "Basic override" {
		t.Fatalf("expected explicit header to win, got %q", headers["Authorization"])
	}
	if headers["X-Scope-OrgID"] != "override-tenant" {
		t.Fatalf("expected explicit tenant header to win, got %q", headers["X-Scope-OrgID"])
	}
}

func base64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

func TestResolveHeadersWithAuthRejectsInvalidConfig(t *testing.T) {
	testCases := []AuthConfig{
		{Mode: ExportAuthModeTenant},
		{Mode: ExportAuthModeBearer},
		{Mode: ExportAuthModeNone, TenantID: "tenant-a"},
		{Mode: ExportAuthModeNone, BearerToken: "token"},
		{Mode: ExportAuthModeNone, BasicUser: "user"},
		{Mode: ExportAuthModeNone, BasicPassword: "secret"},
		{Mode: ExportAuthModeTenant, TenantID: "tenant-a", BearerToken: "token"},
		{Mode: ExportAuthModeBearer, TenantID: "tenant-a", BearerToken: "token"},
		{Mode: ExportAuthMode("unknown"), TenantID: "tenant-a"},
		{Mode: ExportAuthModeBasic},
		{Mode: ExportAuthModeBasic, BasicPassword: "secret"},
	}

	for _, testCase := range testCases {
		_, err := resolveHeadersWithAuth(nil, testCase)
		if err == nil {
			t.Fatalf("expected error for auth config: %+v", testCase)
		}
	}
}

func TestMergeAuthConfigBasicFields(t *testing.T) {
	base := AuthConfig{
		Mode:     ExportAuthModeBearer,
		TenantID: "base-tenant",
	}
	override := AuthConfig{
		Mode:          ExportAuthModeBasic,
		TenantID:      "override-tenant",
		BasicUser:     "probe-user",
		BasicPassword: "secret",
	}
	got := mergeAuthConfig(base, override)

	if got.Mode != ExportAuthModeBasic {
		t.Fatalf("Mode=%q, want %q", got.Mode, ExportAuthModeBasic)
	}
	if got.TenantID != "override-tenant" {
		t.Fatalf("TenantID=%q, want %q", got.TenantID, "override-tenant")
	}
	if got.BasicUser != "probe-user" {
		t.Fatalf("BasicUser=%q, want %q", got.BasicUser, "probe-user")
	}
	if got.BasicPassword != "secret" {
		t.Fatalf("BasicPassword=%q, want %q", got.BasicPassword, "secret")
	}
}

func TestMergeAuthConfigPreservesBaseBasicFields(t *testing.T) {
	base := AuthConfig{
		Mode:          ExportAuthModeBasic,
		BasicUser:     "base-user",
		BasicPassword: "base-secret",
	}
	override := AuthConfig{}
	got := mergeAuthConfig(base, override)

	if got.BasicUser != "base-user" {
		t.Fatalf("BasicUser=%q, want %q", got.BasicUser, "base-user")
	}
	if got.BasicPassword != "base-secret" {
		t.Fatalf("BasicPassword=%q, want %q", got.BasicPassword, "base-secret")
	}
}

func TestNewClientPanicsOnInvalidAuthConfig(t *testing.T) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatalf("expected panic for invalid auth config")
		}
		if !strings.Contains(recovered.(string), "invalid generation auth config") {
			t.Fatalf("unexpected panic message: %v", recovered)
		}
	}()

	_ = NewClient(Config{
		Tracer: noop.NewTracerProvider().Tracer("test"),
		GenerationExport: GenerationExportConfig{
			Protocol:        GenerationExportProtocolHTTP,
			Endpoint:        "http://localhost:8080/api/v1/generations:export",
			Auth:            AuthConfig{Mode: ExportAuthModeTenant},
			BatchSize:       1,
			FlushInterval:   time.Second,
			QueueSize:       1,
			MaxRetries:      1,
			InitialBackoff:  time.Millisecond,
			MaxBackoff:      2 * time.Millisecond,
			PayloadMaxBytes: 1 << 20,
		},
		testGenerationExporter: &capturingGenerationExporter{},
		testDisableWorker:      true,
		Now:                    time.Now,
	})
}

func TestNewClientAppliesPerExportAuthToGenerationExporter(t *testing.T) {
	client := NewClient(Config{
		Tracer: noop.NewTracerProvider().Tracer("test"),
		GenerationExport: GenerationExportConfig{
			Protocol:        GenerationExportProtocolHTTP,
			Endpoint:        "http://localhost:8080/api/v1/generations:export",
			Auth:            AuthConfig{Mode: ExportAuthModeTenant, TenantID: "tenant-a"},
			BatchSize:       1,
			FlushInterval:   time.Second,
			QueueSize:       1,
			MaxRetries:      1,
			InitialBackoff:  time.Millisecond,
			MaxBackoff:      2 * time.Millisecond,
			PayloadMaxBytes: 1 << 20,
		},
		testGenerationExporter: &capturingGenerationExporter{},
		testDisableWorker:      true,
		Now:                    time.Now,
	})
	t.Cleanup(func() {
		_ = client.Shutdown(context.Background())
	})

	if got := client.config.GenerationExport.Headers[tenantHeaderName]; got != "tenant-a" {
		t.Fatalf("expected generation tenant header tenant-a, got %q", got)
	}
}
