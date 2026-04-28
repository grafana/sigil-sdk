package hook

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/grafana/sigil-sdk/go/sigil"

	"github.com/grafana/sigil-sdk/plugins/cursor/internal/config"
	"github.com/grafana/sigil-sdk/plugins/cursor/internal/fragment"
	"github.com/grafana/sigil-sdk/plugins/cursor/internal/mapper"
	"github.com/grafana/sigil-sdk/plugins/cursor/internal/otel"
)

const otelInstrumentationName = "sigil-cursor"

// setupOTelIfConfigured builds OTel providers from cfg.OTel. Returns nil
// providers (no error) when SIGIL_OTEL_ENDPOINT is unset. Cursor may set
// OTEL_EXPORTER_OTLP_* for its own telemetry — we clear those first so
// SIGIL_OTEL_* always wins.
func setupOTelIfConfigured(ctx context.Context, cfg config.Config, logger *log.Logger) *otel.Providers {
	if cfg.OTel.Endpoint == "" {
		return nil
	}
	for _, k := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"OTEL_EXPORTER_OTLP_HEADERS",
		"OTEL_EXPORTER_OTLP_INSECURE",
	} {
		_ = os.Unsetenv(k)
	}
	user := cfg.OTel.User
	if user == "" {
		user = cfg.User
	}
	password := cfg.OTel.Password
	if password == "" {
		password = cfg.Password
	}
	providers, err := otel.Setup(ctx, otel.Config{
		Endpoint: cfg.OTel.Endpoint,
		User:     user,
		Password: password,
		Insecure: cfg.OTel.Insecure,
	})
	if err != nil {
		logger.Printf("otel: setup: %v", err)
		return nil
	}
	if providers != nil {
		logger.Printf("otel: endpoint=%s", cfg.OTel.Endpoint)
	}
	return providers
}

// buildClient constructs the Sigil client from cfg. Wires the OTel providers'
// tracer/meter when present so SDK self-telemetry flows to our OTLP endpoint
// rather than the global noop providers.
func buildClient(cfg config.Config, providers *otel.Providers) *sigil.Client {
	c := sigil.Config{
		ContentCapture: cfg.ContentCapture,
		GenerationExport: sigil.GenerationExportConfig{
			Protocol: sigil.GenerationExportProtocolHTTP,
			Endpoint: strings.TrimRight(cfg.URL, "/") + "/api/v1/generations:export",
			Auth: sigil.AuthConfig{
				Mode:          sigil.ExportAuthModeBasic,
				BasicUser:     cfg.User,
				BasicPassword: cfg.Password,
				TenantID:      cfg.User,
			},
		},
	}
	if providers != nil {
		c.Tracer = providers.Tracer(otelInstrumentationName)
		c.Meter = providers.Meter(otelInstrumentationName)
	}
	return sigil.NewClient(c)
}

// emitGeneration pushes one mapped Generation through the SDK: starts the
// generation span, sets the result, sets a call error if the stop status was
// "error", emits per-tool execute_tool spans, then ends the recorder.
//
// Flushing/shutdown is the caller's responsibility — sessionEnd batches
// multiple generations through one client.
func emitGeneration(ctx context.Context, client *sigil.Client, frag *fragment.Fragment, mapped mapper.Mapped, logger *log.Logger) error {
	genCtx, rec := client.StartGeneration(ctx, mapped.Start)
	rec.SetResult(mapped.Generation, nil)
	if mapped.CallError != nil {
		rec.SetCallError(mapped.CallError)
	}
	emitToolSpans(genCtx, client, frag, mapped.Generation, logger)
	rec.End()
	if err := rec.Err(); err != nil {
		return fmt.Errorf("recorder: %w", err)
	}
	return nil
}

// emitToolSpans creates one execute_tool span per tool invocation in the
// fragment. Spans are anchored at the generation's completed_at so they land
// on the same timeline as the parent generation span.
//
// Tool argument/result content is forwarded as-is. Capture-mode clamping
// happens at the fragment-write boundary (postToolUse drops bytes for any
// mode other than `full`), so by the time we emit, t.ToolInput/Output are
// already empty in metadata_only / no_tool_content. The SDK additionally
// honors Generation.ContentCapture when serializing the span.
func emitToolSpans(ctx context.Context, client *sigil.Client, frag *fragment.Fragment, gen sigil.Generation, logger *log.Logger) {
	for i := range frag.Tools {
		t := &frag.Tools[i]
		if t.ToolName == "" {
			continue
		}
		startedAt := gen.CompletedAt
		if t.DurationMs != nil && !gen.CompletedAt.IsZero() {
			startedAt = gen.CompletedAt.Add(-time.Duration(*t.DurationMs) * time.Millisecond)
		}
		_, toolRec := client.StartToolExecution(ctx, sigil.ToolExecutionStart{
			ToolName:        t.ToolName,
			ToolCallID:      t.ToolUseID,
			ToolType:        "function",
			ConversationID:  gen.ConversationID,
			AgentName:       gen.AgentName,
			AgentVersion:    gen.AgentVersion,
			RequestModel:    gen.Model.Name,
			RequestProvider: gen.Model.Provider,
			StartedAt:       startedAt,
		})

		end := sigil.ToolExecutionEnd{CompletedAt: gen.CompletedAt}
		if len(t.ToolInput) > 0 {
			end.Arguments = string(t.ToolInput)
		}
		if len(t.ToolOutput) > 0 {
			end.Result = string(t.ToolOutput)
		}
		if t.Status == "error" {
			toolRec.SetExecError(toolErrorOr(t.ErrorMessage))
		}
		toolRec.SetResult(end)
		toolRec.End()
		if err := toolRec.Err(); err != nil {
			logger.Printf("tool span enqueue: %v", err)
		}
	}
}

// toolErrorOr wraps an error message string into an error value, with a
// generic sentinel when the message is empty.
func toolErrorOr(msg string) error {
	if msg == "" {
		return errToolError
	}
	return errors.New(msg)
}

var errToolError = errors.New("tool returned error")
