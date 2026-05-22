// Package sigilemit holds the Sigil emission mechanics shared by the per-turn
// agent adapters (codex, copilot, cursor): OTel provider setup, Sigil client
// construction, the generation recorder lifecycle, and tool-span helpers.
//
// The adapters differ in their per-agent options — codex tunes the export
// queue and supplies explicit basic-auth credentials, copilot and cursor lean
// on the SDK's SIGIL_* env resolution — so the client builder takes a hook the
// caller uses to adjust the generation export config. Content handling that is
// genuinely agent-specific (redaction, capture-mode clamping) stays at the
// call site; this package only owns the mechanics that are identical.
package sigilemit

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/grafana/sigil-sdk/go/sigil"

	"github.com/grafana/sigil-sdk/plugins/sigil/internal/otel"
	"github.com/grafana/sigil-sdk/plugins/sigil/internal/timeutil"
)

// ExportEndpoint returns the generations export URL derived from SIGIL_ENDPOINT
// (trailing slash trimmed) plus the SDK's `/api/v1/generations:export` path.
func ExportEndpoint() string {
	return strings.TrimRight(os.Getenv("SIGIL_ENDPOINT"), "/") + "/api/v1/generations:export"
}

// ClientOptions configures NewClient.
type ClientOptions struct {
	// InstrumentationName is the OTel scope name attached to spans/metrics.
	InstrumentationName string
	// ContentCapture is the resolved capture mode for the client.
	ContentCapture sigil.ContentCaptureMode
	// Logger is forwarded to the SDK client. nil leaves the SDK silent
	// (cursor relies on this).
	Logger *log.Logger
	// Providers wires the tracer/meter when non-nil.
	Providers *otel.Providers
	// Export, when non-nil, is called with the base generation export config
	// (HTTP protocol, ExportEndpoint, basic-auth mode) so an agent can layer
	// on queue/retry tuning and explicit credentials before the client is
	// built.
	Export func(*sigil.GenerationExportConfig)
}

// NewClient builds a Sigil client with the shared HTTP/basic-auth generation
// export defaults, applying the caller's Export hook and OTel providers.
func NewClient(opts ClientOptions) *sigil.Client {
	export := sigil.GenerationExportConfig{
		Protocol: sigil.GenerationExportProtocolHTTP,
		Endpoint: ExportEndpoint(),
		Auth:     sigil.AuthConfig{Mode: sigil.ExportAuthModeBasic},
	}
	if opts.Export != nil {
		opts.Export(&export)
	}
	c := sigil.Config{
		ContentCapture:   opts.ContentCapture,
		Logger:           opts.Logger,
		GenerationExport: export,
	}
	if opts.Providers != nil {
		c.Tracer = opts.Providers.Tracer(opts.InstrumentationName)
		c.Meter = opts.Providers.Meter(opts.InstrumentationName)
	}
	return sigil.NewClient(c)
}

// SetupOTel builds OTel providers when an OTLP endpoint is configured
// (SIGIL_OTEL_EXPORTER_OTLP_ENDPOINT or OTEL_EXPORTER_OTLP_ENDPOINT) and
// returns nil otherwise. Setup failures are logged and swallowed so a hook
// keeps exporting generations even when tracing/metrics can't start.
func SetupOTel(ctx context.Context, logger *log.Logger) *otel.Providers {
	endpoint := otel.EndpointFromEnv()
	if endpoint == "" {
		return nil
	}
	providers, err := otel.Setup(ctx)
	if err != nil {
		logger.Printf("otel: setup: %v", err)
		return nil
	}
	if providers != nil {
		logger.Printf("otel: endpoint=%s", endpoint)
	}
	return providers
}

// Record runs the recorder lifecycle for one mapped generation:
// StartGeneration → SetResult → (SetCallError when callErr != nil) → tool spans
// → End → Err. emitTools is invoked with the generation context so per-tool
// spans nest under the generation span; pass nil when there are no tool spans.
// The recorder's Err is wrapped and returned so callers don't silently drop SDK
// validation/enqueue errors.
func Record(
	ctx context.Context,
	client *sigil.Client,
	start sigil.GenerationStart,
	gen sigil.Generation,
	callErr error,
	emitTools func(genCtx context.Context),
) error {
	genCtx, rec := client.StartGeneration(ctx, start)
	rec.SetResult(gen, nil)
	if callErr != nil {
		rec.SetCallError(callErr)
	}
	if emitTools != nil {
		emitTools(genCtx)
	}
	rec.End()
	if err := rec.Err(); err != nil {
		return fmt.Errorf("recorder: %w", err)
	}
	return nil
}

// ToolSpanWindow computes the (startedAt, completedAt) wall-clock window for a
// tool execution span. completedAt is parsed from completedAtRaw, falling back
// to genCompletedAt; startedAt equals completedAt unless a positive duration is
// known, in which case it is backdated by durationMs so the span has real width
// on the timeline.
//
// copilot needs a different window (it records a per-tool StartedAt and uses it
// as the base) and keeps its own helper.
func ToolSpanWindow(completedAtRaw string, durationMs *float64, genCompletedAt time.Time) (startedAt, completedAt time.Time) {
	completedAt = timeutil.ParseTimestamp(completedAtRaw, genCompletedAt)
	startedAt = completedAt
	if durationMs != nil && !completedAt.IsZero() {
		startedAt = completedAt.Add(-time.Duration(*durationMs) * time.Millisecond)
	}
	return startedAt, completedAt
}

var errToolReturned = errors.New("tool returned error")

// ToolError converts a tool error message into an error value, substituting a
// generic sentinel when msg is empty. copilot trims whitespace before the empty
// check and keeps its own helper.
func ToolError(msg string) error {
	if msg == "" {
		return errToolReturned
	}
	return errors.New(msg)
}
