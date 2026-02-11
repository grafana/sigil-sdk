package sigil

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Config controls Sigil client behavior.
type Config struct {
	// OTLPHTTPEndpoint is the OTLP HTTP traces endpoint used by Sigil services.
	OTLPHTTPEndpoint string
	// RecordsEndpoint is the records API endpoint used for payload externalization.
	RecordsEndpoint string
	// PayloadMaxBytes is the max payload size before externalization.
	PayloadMaxBytes int
	// RecordStore persists externalized artifacts.
	RecordStore RecordStore
	// Tracer is used to create GenAI spans around StartGeneration/End.
	Tracer trace.Tracer
	// Now controls clock behavior (useful for tests).
	Now func() time.Time
}

const instrumentationName = "github.com/grafana/sigil/sdks/go/sigil"
const spanAttrGenerationID = "sigil.generation.id"
const spanAttrGenerationMode = "sigil.generation.mode"

type generationMode string

const (
	generationModeSync   generationMode = "generation"
	generationModeStream generationMode = "streaming_generation"
)

// DefaultConfig returns a production-ready baseline configuration.
func DefaultConfig() Config {
	return Config{
		OTLPHTTPEndpoint: "http://localhost:4318/v1/traces",
		RecordsEndpoint:  "http://localhost:8080/api/v1/records",
		PayloadMaxBytes:  8192,
		RecordStore:      NewMemoryRecordStore(),
		Tracer:           otel.Tracer(instrumentationName),
		Now:              time.Now,
	}
}

// Client records normalized generation data and GenAI spans.
type Client struct {
	config Config
}

// GenerationRecorder records and closes one in-flight generation span.
// A recorder is single-use; calling End more than once returns an error.
type GenerationRecorder struct {
	client    *Client
	ctx       context.Context
	span      trace.Span
	seed      GenerationStart
	mode      generationMode
	startedAt time.Time

	mu             sync.Mutex
	ended          bool
	lastGeneration Generation
}

// NewClient creates a Client, applying defaults for empty config values.
func NewClient(config Config) *Client {
	cfg := config
	defaults := DefaultConfig()

	if cfg.OTLPHTTPEndpoint == "" {
		cfg.OTLPHTTPEndpoint = defaults.OTLPHTTPEndpoint
	}
	if cfg.RecordsEndpoint == "" {
		cfg.RecordsEndpoint = defaults.RecordsEndpoint
	}
	if cfg.RecordStore == nil {
		cfg.RecordStore = defaults.RecordStore
	}
	if cfg.Tracer == nil {
		cfg.Tracer = defaults.Tracer
	}
	if cfg.Now == nil {
		cfg.Now = defaults.Now
	}

	return &Client{
		config: cfg,
	}
}

// StartGeneration starts a GenAI span and returns a context to use for the provider call.
//
// Start fields are seeds: End can fill any zero-valued generation fields from start.
//
// Linking is two-way after End:
//   - Generation.TraceID and Generation.SpanID are set from the created span context.
//   - The span includes sigil.generation.id as an attribute.
func (c *Client) StartGeneration(ctx context.Context, start GenerationStart) (context.Context, *GenerationRecorder, error) {
	return c.startGeneration(ctx, start, generationModeSync)
}

// StartStreamingGeneration starts a GenAI span for streaming provider calls.
func (c *Client) StartStreamingGeneration(ctx context.Context, start GenerationStart) (context.Context, *GenerationRecorder, error) {
	return c.startGeneration(ctx, start, generationModeStream)
}

func (c *Client) startGeneration(ctx context.Context, start GenerationStart, mode generationMode) (context.Context, *GenerationRecorder, error) {
	if c == nil {
		return nil, nil, errors.New("sigil client is nil")
	}
	if mode == "" {
		mode = generationModeSync
	}

	seed := cloneGenerationStart(start)
	if seed.OperationName == "" {
		seed.OperationName = defaultOperationName
	}

	startedAt := seed.StartedAt
	if startedAt.IsZero() {
		startedAt = c.now().UTC()
	} else {
		startedAt = startedAt.UTC()
	}
	seed.StartedAt = startedAt

	callCtx, span := c.startSpan(ctx, seed.OperationName, startedAt)
	span.SetAttributes(generationSpanAttributes(Generation{
		ID:            seed.ID,
		OperationName: seed.OperationName,
		Model:         seed.Model,
	}, mode)...)

	return callCtx, &GenerationRecorder{
		client:    c,
		ctx:       callCtx,
		span:      span,
		seed:      seed,
		mode:      mode,
		startedAt: startedAt,
	}, nil
}

// End finalizes generation recording, sets span status, and closes the span.
//
// End is single-use. A second call returns an error and does nothing.
func (r *GenerationRecorder) End(g Generation, callErr error) error {
	if r == nil {
		return errors.New("generation recorder is nil")
	}

	r.mu.Lock()
	if r.ended {
		r.mu.Unlock()
		return errors.New("generation recorder already ended")
	}
	r.ended = true
	r.mu.Unlock()
	if r.client == nil || r.span == nil {
		return errors.New("generation recorder is not initialized")
	}

	completedAt := r.client.now().UTC()
	generation := r.normalizeGeneration(g, completedAt, callErr)
	applyTraceContextFromSpan(r.span, &generation)

	r.span.SetName(generationSpanName(generation))
	r.span.SetAttributes(generationSpanAttributes(generation, r.mode)...)

	r.mu.Lock()
	r.lastGeneration = cloneGeneration(generation)
	r.mu.Unlock()

	recordErr := r.client.persistGeneration(r.ctx, generation)
	if callErr != nil {
		r.span.RecordError(callErr)
	}
	if recordErr != nil {
		r.span.RecordError(recordErr)
	}
	switch {
	case callErr != nil:
		r.span.SetStatus(codes.Error, callErr.Error())
	case recordErr != nil:
		r.span.SetStatus(codes.Error, recordErr.Error())
	default:
		r.span.SetStatus(codes.Ok, "")
	}
	r.span.End(trace.WithTimestamp(generation.CompletedAt))

	return combineErrors(callErr, recordErr)
}

func (r *GenerationRecorder) normalizeGeneration(raw Generation, completedAt time.Time, callErr error) Generation {
	g := cloneGeneration(raw)

	if g.ID == "" {
		g.ID = r.seed.ID
	}
	if g.ID == "" {
		g.ID = newRandomID("gen")
	}
	if g.ThreadID == "" {
		g.ThreadID = r.seed.ThreadID
	}
	if g.OperationName == "" {
		g.OperationName = r.seed.OperationName
	}
	if g.OperationName == "" {
		g.OperationName = defaultOperationName
	}
	if g.Model.Provider == "" {
		g.Model.Provider = r.seed.Model.Provider
	}
	if g.Model.Name == "" {
		g.Model.Name = r.seed.Model.Name
	}
	if g.SystemPrompt == "" {
		g.SystemPrompt = r.seed.SystemPrompt
	}
	if len(g.Tools) == 0 {
		g.Tools = cloneTools(r.seed.Tools)
	}
	g.Tags = mergeTags(r.seed.Tags, g.Tags)
	g.Metadata = mergeMetadata(r.seed.Metadata, g.Metadata)

	if g.StartedAt.IsZero() {
		g.StartedAt = r.startedAt
	} else {
		g.StartedAt = g.StartedAt.UTC()
	}
	if g.CompletedAt.IsZero() {
		g.CompletedAt = completedAt
	} else {
		g.CompletedAt = g.CompletedAt.UTC()
	}

	if callErr != nil {
		if g.CallError == "" {
			g.CallError = callErr.Error()
		}
		if g.Metadata == nil {
			g.Metadata = map[string]any{}
		}
		g.Metadata["call_error"] = callErr.Error()
	}

	g.Usage = g.Usage.Normalize()
	return g
}

func combineErrors(callErr, recordErr error) error {
	switch {
	case callErr != nil && recordErr != nil:
		return errors.Join(callErr, fmt.Errorf("record generation: %w", recordErr))
	case callErr != nil:
		return callErr
	case recordErr != nil:
		return recordErr
	default:
		return nil
	}
}

func (c *Client) persistGeneration(ctx context.Context, generation Generation) error {
	if err := ValidateGeneration(generation); err != nil {
		return err
	}

	_, err := c.externalizeArtifacts(ctx, generation.Artifacts)
	return err
}

func (c *Client) now() time.Time {
	return c.config.Now()
}

func (c *Client) startSpan(ctx context.Context, operation string, startedAt time.Time) (context.Context, trace.Span) {
	if ctx == nil {
		ctx = context.Background()
	}

	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithTimestamp(startedAt),
	}

	return c.config.Tracer.Start(ctx, generationSpanName(Generation{OperationName: operation}), opts...)
}

func generationSpanName(g Generation) string {
	operation := strings.TrimSpace(g.OperationName)
	if operation == "" {
		operation = defaultOperationName
	}

	return "gen_ai." + operation
}

func generationSpanAttributes(g Generation, mode generationMode) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String("gen_ai.operation.name", operationName(g)),
		attribute.String(spanAttrGenerationMode, string(mode)),
	}
	if g.ID != "" {
		attrs = append(attrs, attribute.String(spanAttrGenerationID, g.ID))
	}

	if provider := strings.TrimSpace(g.Model.Provider); provider != "" {
		attrs = append(attrs, attribute.String("gen_ai.provider.name", provider))
	}
	if model := strings.TrimSpace(g.Model.Name); model != "" {
		attrs = append(attrs, attribute.String("gen_ai.request.model", model))
	}
	if g.StopReason != "" {
		attrs = append(attrs,
			attribute.String("gen_ai.response.finish_reason", g.StopReason),
			attribute.StringSlice("gen_ai.response.finish_reasons", []string{g.StopReason}),
		)
	}
	if g.Usage.InputTokens != 0 {
		attrs = append(attrs, attribute.Int64("gen_ai.usage.input_tokens", g.Usage.InputTokens))
	}
	if g.Usage.OutputTokens != 0 {
		attrs = append(attrs, attribute.Int64("gen_ai.usage.output_tokens", g.Usage.OutputTokens))
	}
	if g.Usage.TotalTokens != 0 {
		attrs = append(attrs, attribute.Int64("gen_ai.usage.total_tokens", g.Usage.TotalTokens))
	}
	if g.Usage.ReasoningTokens != 0 {
		attrs = append(attrs, attribute.Int64("gen_ai.usage.reasoning_tokens", g.Usage.ReasoningTokens))
	}

	return attrs
}

func operationName(g Generation) string {
	operation := strings.TrimSpace(g.OperationName)
	if operation == "" {
		return defaultOperationName
	}

	return operation
}

func applyTraceContextFromSpan(span trace.Span, generation *Generation) {
	if generation == nil || span == nil {
		return
	}

	spanContext := span.SpanContext()
	if !spanContext.IsValid() {
		return
	}

	generation.TraceID = spanContext.TraceID().String()
	generation.SpanID = spanContext.SpanID().String()
}

func mergeTags(base, override map[string]string) map[string]string {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}

	out := cloneTags(base)
	if out == nil {
		out = map[string]string{}
	}
	for key, value := range override {
		out[key] = value
	}
	return out
}

func mergeMetadata(base, override map[string]any) map[string]any {
	if len(base) == 0 && len(override) == 0 {
		return nil
	}

	out := cloneMetadata(base)
	if out == nil {
		out = map[string]any{}
	}
	for key, value := range override {
		out[key] = value
	}
	return out
}

func (c *Client) externalizeArtifacts(ctx context.Context, artifacts []Artifact) ([]ArtifactRef, error) {
	if len(artifacts) == 0 {
		return nil, nil
	}

	if c.config.RecordStore == nil {
		return nil, nil
	}

	refs := make([]ArtifactRef, 0, len(artifacts))
	for i := range artifacts {
		if len(artifacts[i].Payload) == 0 && artifacts[i].RecordID == "" {
			continue
		}

		recordID := artifacts[i].RecordID
		if recordID == "" {
			recordID = newRandomID("rec")

			record := Record{
				ID:          recordID,
				Kind:        artifacts[i].Kind,
				Name:        artifacts[i].Name,
				ContentType: contentTypeOrDefault(artifacts[i].ContentType),
				Payload:     append([]byte(nil), artifacts[i].Payload...),
				CreatedAt:   c.now().UTC(),
			}

			if _, err := c.config.RecordStore.Put(ctx, record); err != nil {
				return nil, fmt.Errorf("store artifact[%d]: %w", i, err)
			}
		}

		uri := artifacts[i].URI
		if uri == "" {
			uri = "sigil://record/" + recordID
		}

		refs = append(refs, ArtifactRef{
			Kind:        artifacts[i].Kind,
			Name:        artifacts[i].Name,
			ContentType: contentTypeOrDefault(artifacts[i].ContentType),
			RecordID:    recordID,
			URI:         uri,
		})
	}

	return refs, nil
}

func contentTypeOrDefault(contentType string) string {
	if contentType != "" {
		return contentType
	}

	return "application/json"
}
