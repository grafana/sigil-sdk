package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/grafana/sigil-sdk/go/sigil"
	"github.com/grafana/sigil-sdk/plugins/claude-code/internal/mapper"
	"github.com/grafana/sigil-sdk/plugins/claude-code/internal/otel"
	"github.com/grafana/sigil-sdk/plugins/claude-code/internal/redact"
	"github.com/grafana/sigil-sdk/plugins/claude-code/internal/state"
	"github.com/grafana/sigil-sdk/plugins/claude-code/internal/transcript"
)

type hookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
}

var logger *log.Logger

func initLogger() {
	logger = log.New(io.Discard, "sigil-cc: ", log.Ltime)

	if !strings.EqualFold(os.Getenv("SIGIL_DEBUG"), "true") {
		return
	}

	dir := filepath.Join(os.Getenv("HOME"), ".claude", "state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "sigil-cc.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	logger = log.New(f, "sigil-cc: ", log.Ldate|log.Ltime)
}

func main() {
	initLogger()
	run()
}

func run() {
	input, err := parseStdin()
	if err != nil {
		logger.Printf("stdin: %v", err)
		return
	}
	logger.Printf("session=%s transcript=%s", input.SessionID, input.TranscriptPath)

	sigilURL := os.Getenv("SIGIL_URL")
	sigilUser := os.Getenv("SIGIL_USER")
	sigilPassword := os.Getenv("SIGIL_PASSWORD")

	if sigilURL == "" || sigilUser == "" || sigilPassword == "" {
		return
	}

	contentMode := resolveContentMode()
	extraTags := parseExtraTags(os.Getenv("SIGIL_EXTRA_TAGS"))
	userID := resolveUserID()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ctx = sigil.WithUserID(ctx, userID)

	// The Go OTLP SDK reads standard OTEL_EXPORTER_OTLP_* env vars automatically,
	// which Claude Code sets for its own telemetry. Clear them so our SIGIL_OTEL_*
	// config takes effect.
	for _, k := range []string{
		"OTEL_EXPORTER_OTLP_ENDPOINT",
		"OTEL_EXPORTER_OTLP_PROTOCOL",
		"OTEL_EXPORTER_OTLP_HEADERS",
		"OTEL_EXPORTER_OTLP_INSECURE",
	} {
		_ = os.Unsetenv(k)
	}

	otelEndpoint := os.Getenv("SIGIL_OTEL_ENDPOINT")
	otelProviders, err := otel.Setup(ctx, otel.Config{
		Endpoint: otelEndpoint,
		User:     envOr("SIGIL_OTEL_USER", sigilUser),
		Password: envOr("SIGIL_OTEL_PASSWORD", sigilPassword),
		Insecure: strings.EqualFold(os.Getenv("SIGIL_OTEL_INSECURE"), "true"),
	})
	if err != nil {
		logger.Printf("otel setup: %v", err)
	} else if otelProviders != nil {
		logger.Printf("otel: endpoint=%s", otelEndpoint)
	}
	defer func() { _ = otelProviders.Shutdown(ctx) }()

	st := state.Load(input.SessionID)

	lines, _, err := transcript.Read(input.TranscriptPath, st.Offset)
	if err != nil {
		logger.Printf("read transcript: %v", err)
		return
	}
	if len(lines) == 0 {
		return
	}
	logger.Printf("read %d raw lines", len(lines))

	lines, safeOffset := mapper.Coalesce(lines)
	if safeOffset == 0 {
		return
	}
	logger.Printf("coalesced to %d lines, safe offset=%d", len(lines), safeOffset)

	var r *redact.Redactor
	if contentMode != sigil.ContentCaptureModeMetadataOnly {
		r = redact.New()
	}

	gens := mapper.Process(lines, &st, mapper.Options{
		SessionID: input.SessionID,
		Logger:    logger,
		ExtraTags: extraTags,
	}, r)

	if len(gens) == 0 {
		st.Offset = safeOffset
		_ = state.Save(input.SessionID, st)
		return
	}
	logger.Printf("produced %d generations", len(gens))

	cfg := sigil.Config{
		ContentCapture: contentMode,
		GenerationExport: sigil.GenerationExportConfig{
			Protocol: sigil.GenerationExportProtocolHTTP,
			Endpoint: sigilURL + "/api/v1/generations:export",
			Auth: sigil.AuthConfig{
				Mode:          sigil.ExportAuthModeBasic,
				BasicUser:     sigilUser,
				BasicPassword: sigilPassword,
				TenantID:      sigilUser,
			},
		},
	}

	if otelProviders != nil {
		cfg.Tracer = otelProviders.Tracer("sigil-cc")
		cfg.Meter = otelProviders.Meter("sigil-cc")
	}

	client := sigil.NewClient(cfg)
	t0 := time.Now()

	toolResults := buildToolResultMap(gens)

	for _, gen := range gens {
		genStart := sigil.GenerationStart{
			ID:                gen.ID,
			ConversationID:    gen.ConversationID,
			ConversationTitle: gen.ConversationTitle,
			AgentName:         gen.AgentName,
			AgentVersion:      gen.AgentVersion,
			Mode:              gen.Mode,
			OperationName:     gen.OperationName,
			Model:             gen.Model,
			Tags:              gen.Tags,
			Metadata:          gen.Metadata,
		}

		genCtx, rec := client.StartGeneration(ctx, genStart)
		rec.SetResult(gen, nil)

		emitToolSpans(genCtx, client, gen, toolResults)

		rec.End()

		if err := rec.Err(); err != nil {
			logger.Printf("enqueue: %v", err)
		}
	}

	if err := client.Flush(ctx); err != nil {
		logger.Printf("flush: %v", err)
		_ = client.Shutdown(ctx)
		return
	}
	_ = client.Shutdown(ctx)

	if otelProviders != nil {
		if err := otelProviders.ForceFlush(); err != nil {
			logger.Printf("otel flush: %v", err)
		}
	}

	st.Offset = safeOffset
	if err := state.Save(input.SessionID, st); err != nil {
		logger.Printf("save state: %v", err)
	}
	logger.Printf("done: %d generations in %s", len(gens), time.Since(t0))
}

func parseStdin() (*hookInput, error) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("empty stdin")
	}

	var input hookInput
	if err := json.Unmarshal(data, &input); err != nil {
		return nil, err
	}

	if input.SessionID == "" || input.TranscriptPath == "" {
		return nil, fmt.Errorf("missing session_id or transcript_path")
	}

	return &input, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// parseExtraTags parses a comma-separated "key=value" string into a tag map.
// Malformed entries (empty keys, missing '=', empty values) are silently skipped —
// this runs in a stop hook where logging warnings adds noise for no user benefit.
// Empty input returns nil so the mapper can short-circuit on the zero-extras path.
func parseExtraTags(s string) map[string]string {
	if s == "" {
		return nil
	}
	out := make(map[string]string)
	for _, pair := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// buildToolResultMap indexes tool results by their call ID across all generations.
// Tool results appear in the Input of the generation that follows the tool call.
func buildToolResultMap(gens []sigil.Generation) map[string]*sigil.ToolResult {
	m := make(map[string]*sigil.ToolResult)
	for _, gen := range gens {
		for _, msg := range gen.Input {
			for i, part := range msg.Parts {
				if part.ToolResult != nil && part.ToolResult.ToolCallID != "" {
					m[part.ToolResult.ToolCallID] = msg.Parts[i].ToolResult
				}
			}
		}
	}
	return m
}

// emitToolSpans creates execute_tool spans for each tool call in the generation output.
func emitToolSpans(ctx context.Context, client *sigil.Client, gen sigil.Generation, results map[string]*sigil.ToolResult) {
	for _, msg := range gen.Output {
		for _, part := range msg.Parts {
			if part.ToolCall == nil {
				continue
			}
			tc := part.ToolCall
			start := sigil.ToolExecutionStart{
				ToolName:        tc.Name,
				ToolCallID:      tc.ID,
				ToolType:        "function",
				ConversationID:  gen.ConversationID,
				AgentName:       gen.AgentName,
				AgentVersion:    gen.AgentVersion,
				RequestModel:    gen.Model.Name,
				RequestProvider: gen.Model.Provider,
				StartedAt:       gen.CompletedAt,
			}
			_, toolRec := client.StartToolExecution(ctx, start)

			end := sigil.ToolExecutionEnd{
				CompletedAt: gen.CompletedAt,
				Arguments:   string(tc.InputJSON),
			}
			if tr, ok := results[tc.ID]; ok {
				if tr.Content != "" {
					end.Result = tr.Content
				} else if len(tr.ContentJSON) > 0 {
					end.Result = string(tr.ContentJSON)
				}
			}

			if tr, ok := results[tc.ID]; ok && tr.IsError {
				toolRec.SetExecError(fmt.Errorf("tool returned error"))
			}

			toolRec.SetResult(end)
			toolRec.End()
		}
	}
}

// resolveContentMode returns the effective ContentCaptureMode from environment.
// Priority: SIGIL_CONTENT_CAPTURE_MODE > SIGIL_CONTENT_CAPTURE (legacy) > MetadataOnly.
func resolveContentMode() sigil.ContentCaptureMode {
	if v := os.Getenv("SIGIL_CONTENT_CAPTURE_MODE"); v != "" {
		var mode sigil.ContentCaptureMode
		if err := mode.UnmarshalText([]byte(v)); err != nil {
			return sigil.ContentCaptureModeMetadataOnly
		}
		return mode
	}
	// Backward compat: SIGIL_CONTENT_CAPTURE=true maps to Full.
	if strings.EqualFold(os.Getenv("SIGIL_CONTENT_CAPTURE"), "true") {
		return sigil.ContentCaptureModeFull
	}
	return sigil.ContentCaptureModeMetadataOnly
}
