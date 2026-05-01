package otel

import "testing"

func TestParseEndpointTrimsTrailingSlash(t *testing.T) {
	host, tracePath, metricPath, insecure := parseEndpoint("https://gateway.example.com/otlp/", false)

	if host != "gateway.example.com" {
		t.Fatalf("host = %q, want %q", host, "gateway.example.com")
	}
	if tracePath != "/otlp/v1/traces" {
		t.Fatalf("tracePath = %q, want %q", tracePath, "/otlp/v1/traces")
	}
	if metricPath != "/otlp/v1/metrics" {
		t.Fatalf("metricPath = %q, want %q", metricPath, "/otlp/v1/metrics")
	}
	if insecure {
		t.Fatalf("insecure = %t, want false", insecure)
	}
}
