package otel

import (
	"context"

	"github.com/grafana/sigil-sdk/go/otelutil"
)

type Config = otelutil.Config

type Providers = otelutil.Providers

// Setup creates OTLP HTTP trace and metric providers.
// Returns nil and no error if endpoint is empty.
func Setup(ctx context.Context, cfg Config) (*Providers, error) {
	return otelutil.Setup(ctx, cfg, "sigil-cc")
}
