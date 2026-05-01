package genkit

import (
	"strings"

	"github.com/grafana/sigil-sdk/go/sigil"
)

// Options configures the Genkit Sigil plugin.
type Options struct {
	AgentName      string
	AgentVersion   string
	ConversationID string
	// ContentCapture overrides the client-level content capture mode for
	// generations recorded by this plugin. Default (zero value) inherits
	// from the client's Config.ContentCapture.
	ContentCapture sigil.ContentCaptureMode
	ExtraTags      map[string]string
	ExtraMetadata  map[string]any
}

func parseModelRef(name string) sigil.ModelRef {
	name = strings.TrimSpace(name)
	if idx := strings.Index(name, "/"); idx >= 0 {
		return sigil.ModelRef{
			Provider: name[:idx],
			Name:     name[idx+1:],
		}
	}
	return sigil.ModelRef{Provider: name, Name: name}
}

