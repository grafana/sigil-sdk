package otel

import (
	"context"

	"github.com/grafana/sigil-sdk/go/otelutil"
)

type Config = otelutil.Config

type Providers = otelutil.Providers

func Setup(ctx context.Context, cfg Config) (*Providers, error) {
	return otelutil.Setup(ctx, cfg, "sigil-cursor")
}
