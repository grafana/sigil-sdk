// Package otel sets up OTLP HTTP trace + metric providers for the cursor
// plugin.
//
// The plugin does not invent its own SIGIL_OTEL_* env-var schema. It uses the
// OpenTelemetry-standard OTEL_EXPORTER_OTLP_* vars (read natively by the OTel
// SDK exporters) plus a thin convenience: when OTEL_EXPORTER_OTLP_HEADERS has
// no Authorization entry, the plugin injects a Basic header derived from
// SIGIL_AUTH_TENANT_ID + SIGIL_AUTH_TOKEN so users don't have to hand-encode
// it.
package otel

import (
	"context"
	"encoding/base64"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// envOTLPEndpoint is the canonical OTel env var the SDK exporters read.
const envOTLPEndpoint = "OTEL_EXPORTER_OTLP_ENDPOINT"

// Providers holds initialized OTel providers. All methods are nil-safe.
type Providers struct {
	tp *sdktrace.TracerProvider
	mp *sdkmetric.MeterProvider
}

func (p *Providers) Tracer(name string) trace.Tracer {
	if p == nil {
		return nil
	}
	return p.tp.Tracer(name)
}

func (p *Providers) Meter(name string) metric.Meter {
	if p == nil {
		return nil
	}
	return p.mp.Meter(name)
}

// ForceFlush exports pending traces and metrics concurrently.
func (p *Providers) ForceFlush() error {
	if p == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errc := make(chan error, 2)
	go func() { errc <- p.tp.ForceFlush(ctx) }()
	go func() { errc <- p.mp.ForceFlush(ctx) }()

	var firstErr error
	for range 2 {
		if err := <-errc; err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Shutdown flushes and shuts down both providers.
func (p *Providers) Shutdown(_ context.Context) error {
	if p == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := p.mp.Shutdown(ctx); err != nil {
		_ = p.tp.Shutdown(ctx)
		return err
	}
	return p.tp.Shutdown(ctx)
}

// Setup creates OTLP HTTP trace + metric providers from the OTel-standard
// env vars (OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_EXPORTER_OTLP_HEADERS, etc.).
// Returns nil providers (no error) when no OTLP endpoint is configured.
//
// As a convenience, when OTEL_EXPORTER_OTLP_HEADERS is missing an
// Authorization entry, an `Authorization=Basic base64(tenant:token)` header
// is synthesized from SIGIL_AUTH_TENANT_ID + SIGIL_AUTH_TOKEN before the
// exporter constructors read the env. Users who want a different scheme can
// set OTEL_EXPORTER_OTLP_HEADERS themselves and the plugin won't touch it.
func Setup(ctx context.Context) (*Providers, error) {
	if os.Getenv(envOTLPEndpoint) == "" {
		return nil, nil
	}

	injectAuthHeaderIfMissing()

	if name := os.Getenv("OTEL_SERVICE_NAME"); name == "" {
		_ = os.Setenv("OTEL_SERVICE_NAME", "sigil-cursor")
	}

	setupCtx, setupCancel := context.WithTimeout(ctx, 3*time.Second)
	defer setupCancel()

	traceExp, err := otlptracehttp.New(setupCtx)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp, sdktrace.WithBatchTimeout(1*time.Second)),
	)

	metricExp, err := otlpmetrichttp.New(setupCtx)
	if err != nil {
		_ = tp.Shutdown(ctx)
		return nil, err
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(1*time.Second))),
	)

	return &Providers{tp: tp, mp: mp}, nil
}

// injectAuthHeaderIfMissing adds an Authorization=Basic header to
// OTEL_EXPORTER_OTLP_HEADERS unless one is already present (case-insensitive).
// Falls back silently when SIGIL_AUTH_TENANT_ID + SIGIL_AUTH_TOKEN aren't
// both set — the user is presumed to be running an unauthenticated
// collector and handling auth themselves.
func injectAuthHeaderIfMissing() {
	tenant := os.Getenv("SIGIL_AUTH_TENANT_ID")
	token := os.Getenv("SIGIL_AUTH_TOKEN")
	if tenant == "" || token == "" {
		return
	}
	existing := os.Getenv("OTEL_EXPORTER_OTLP_HEADERS")
	if hasAuthorizationHeader(existing) {
		return
	}
	authHeader := "Authorization=Basic " + base64.StdEncoding.EncodeToString([]byte(tenant+":"+token))
	if existing == "" {
		_ = os.Setenv("OTEL_EXPORTER_OTLP_HEADERS", authHeader)
		return
	}
	_ = os.Setenv("OTEL_EXPORTER_OTLP_HEADERS", existing+","+authHeader)
}

// hasAuthorizationHeader checks whether a CSV header string already contains
// an Authorization key. OTel header names are case-insensitive in practice so
// we compare lowered prefixes.
func hasAuthorizationHeader(headers string) bool {
	for _, pair := range strings.Split(headers, ",") {
		eq := strings.IndexByte(pair, '=')
		if eq < 0 {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(pair[:eq]), "Authorization") {
			return true
		}
	}
	return false
}
