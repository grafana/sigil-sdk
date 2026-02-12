package sigil

import (
	"context"
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

func TestResolveHeadersWithAuthRejectsInvalidConfig(t *testing.T) {
	testCases := []AuthConfig{
		{Mode: ExportAuthModeTenant},
		{Mode: ExportAuthModeBearer},
		{Mode: ExportAuthModeNone, TenantID: "tenant-a"},
		{Mode: ExportAuthModeNone, BearerToken: "token"},
		{Mode: ExportAuthModeTenant, TenantID: "tenant-a", BearerToken: "token"},
		{Mode: ExportAuthModeBearer, TenantID: "tenant-a", BearerToken: "token"},
		{Mode: ExportAuthMode("unknown"), TenantID: "tenant-a"},
	}

	for _, testCase := range testCases {
		_, err := resolveHeadersWithAuth(nil, testCase)
		if err == nil {
			t.Fatalf("expected error for auth config: %+v", testCase)
		}
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

func TestNewClientAppliesPerExportAuthToTraceExporter(t *testing.T) {
	client := NewClient(Config{
		Trace: TraceConfig{
			Protocol: TraceProtocolHTTP,
			Endpoint: "http://localhost:4318/v1/traces",
			Auth: AuthConfig{
				Mode:        ExportAuthModeBearer,
				BearerToken: "trace-secret",
			},
		},
		testGenerationExporter: &capturingGenerationExporter{},
		testDisableWorker:      true,
		Now:                    time.Now,
	})
	t.Cleanup(func() {
		_ = client.Shutdown(context.Background())
	})

	if got := client.config.Trace.Headers[authorizationHeaderName]; got != "Bearer trace-secret" {
		t.Fatalf("expected trace authorization header, got %q", got)
	}
}
