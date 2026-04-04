package otel

import (
	"context"
	"encoding/base64"
	"net/url"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Config holds config for optional OTLP HTTP export.
type Config struct {
	Endpoint string
	User     string
	Password string
	Insecure bool
}

// Providers holds initialized OTel providers.
// All methods are nil-safe.
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

// parseEndpoint extracts host, base path, and TLS setting from an endpoint string.
// Handles both "host:port" and full URLs like "https://gateway.example.com/otlp".
func parseEndpoint(raw string, insecureFlag bool) (host, tracePath, metricPath string, insecure bool) {
	insecure = insecureFlag

	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw, "", "", insecure
	}

	host = u.Host
	if u.Scheme == "http" {
		insecure = true
	}

	if base := u.Path; base != "" && base != "/" {
		tracePath = base + "/v1/traces"
		metricPath = base + "/v1/metrics"
	}

	return host, tracePath, metricPath, insecure
}

// Setup creates OTLP HTTP trace and metric providers.
// Returns nil and no error if endpoint is empty.
func Setup(ctx context.Context, cfg Config) (*Providers, error) {
	if cfg.Endpoint == "" {
		return nil, nil
	}

	host, tracePath, metricPath, insecure := parseEndpoint(cfg.Endpoint, cfg.Insecure)

	res, _ := resource.Merge(
		resource.Default(),
		resource.NewSchemaless(attribute.String("service.name", "sigil-cc")),
	)

	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(cfg.User+":"+cfg.Password))
	headers := map[string]string{"Authorization": auth}

	setupCtx, setupCancel := context.WithTimeout(ctx, 3*time.Second)
	defer setupCancel()

	// Trace exporter
	traceOpts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(host),
		otlptracehttp.WithHeaders(headers),
	}
	if tracePath != "" {
		traceOpts = append(traceOpts, otlptracehttp.WithURLPath(tracePath))
	}
	if insecure {
		traceOpts = append(traceOpts, otlptracehttp.WithInsecure())
	}
	traceExp, err := otlptracehttp.New(setupCtx, traceOpts...)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp, sdktrace.WithBatchTimeout(1*time.Second)),
		sdktrace.WithResource(res),
	)

	// Metric exporter
	metricOpts := []otlpmetrichttp.Option{
		otlpmetrichttp.WithEndpoint(host),
		otlpmetrichttp.WithHeaders(headers),
		otlpmetrichttp.WithTemporalitySelector(func(_ sdkmetric.InstrumentKind) metricdata.Temporality {
			return metricdata.DeltaTemporality
		}),
	}
	if metricPath != "" {
		metricOpts = append(metricOpts, otlpmetrichttp.WithURLPath(metricPath))
	}
	if insecure {
		metricOpts = append(metricOpts, otlpmetrichttp.WithInsecure())
	}
	metricExp, err := otlpmetrichttp.New(setupCtx, metricOpts...)
	if err != nil {
		_ = tp.Shutdown(ctx)
		return nil, err
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(1*time.Second))),
		sdkmetric.WithResource(res),
	)

	return &Providers{tp: tp, mp: mp}, nil
}
