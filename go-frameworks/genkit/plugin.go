package genkit

import (
	"context"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core/api"
	"github.com/grafana/sigil-sdk/go/sigil"
)

// Plugin implements Genkit's api.Plugin interface for Sigil observability.
type Plugin struct {
	client *sigil.Client
	opts   Options
}

// New creates a Sigil plugin for Genkit.
func New(client *sigil.Client, opts Options) *Plugin {
	return &Plugin{
		client: client,
		opts:   opts,
	}
}

func (p *Plugin) Name() string {
	return "sigil"
}

func (p *Plugin) Init(_ context.Context) []api.Action {
	return nil
}

// Middleware returns a ModelMiddleware for the given model.
func (p *Plugin) Middleware(model ai.ModelArg) ai.ModelMiddleware {
	return createMiddleware(p.client, model.Name(), p.opts)
}
