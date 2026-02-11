package sigil

import (
	"context"
	"encoding/json"
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
const (
	spanAttrGenerationID      = "sigil.generation.id"
	spanAttrConversationID    = "gen_ai.conversation.id"
	spanAttrErrorType         = "error.type"
	spanAttrOperationName     = "gen_ai.operation.name"
	spanAttrProviderName      = "gen_ai.provider.name"
	spanAttrRequestModel      = "gen_ai.request.model"
	spanAttrResponseID        = "gen_ai.response.id"
	spanAttrResponseModel     = "gen_ai.response.model"
	spanAttrFinishReasons     = "gen_ai.response.finish_reasons"
	spanAttrInputTokens       = "gen_ai.usage.input_tokens"
	spanAttrOutputTokens      = "gen_ai.usage.output_tokens"
	spanAttrCacheReadTokens   = "gen_ai.usage.cache_read_input_tokens"
	spanAttrCacheWriteTokens  = "gen_ai.usage.cache_write_input_tokens"
	spanAttrToolName          = "gen_ai.tool.name"
	spanAttrToolCallID        = "gen_ai.tool.call.id"
	spanAttrToolType          = "gen_ai.tool.type"
	spanAttrToolDescription   = "gen_ai.tool.description"
	spanAttrToolCallArguments = "gen_ai.tool.call.arguments"
	spanAttrToolCallResult    = "gen_ai.tool.call.result"
)

var (
	errGenerationValidation = errors.New("generation validation failed")
	errGenerationStore      = errors.New("generation store failed")
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
	startedAt time.Time

	mu             sync.Mutex
	ended          bool
	lastGeneration Generation
}

// ToolExecutionRecorder records and closes one in-flight execute_tool span.
// A recorder is single-use; calling End more than once returns an error.
type ToolExecutionRecorder struct {
	client         *Client
	ctx            context.Context
	span           trace.Span
	seed           ToolExecutionStart
	startedAt      time.Time
	includeContent bool

	mu    sync.Mutex
	ended bool
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
	return c.startGeneration(ctx, start)
}

// StartStreamingGeneration starts a GenAI span for streaming provider calls.
func (c *Client) StartStreamingGeneration(ctx context.Context, start GenerationStart) (context.Context, *GenerationRecorder, error) {
	return c.startGeneration(ctx, start)
}

func (c *Client) startGeneration(ctx context.Context, start GenerationStart) (context.Context, *GenerationRecorder, error) {
	if c == nil {
		return nil, nil, errors.New("sigil client is nil")
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

	callCtx, span := c.startSpan(ctx, Generation{
		ID:             seed.ID,
		ConversationID: seed.ConversationID,
		OperationName:  seed.OperationName,
		Model:          seed.Model,
	}, trace.SpanKindClient, startedAt)
	span.SetAttributes(generationSpanAttributes(Generation{
		ID:             seed.ID,
		ConversationID: seed.ConversationID,
		OperationName:  seed.OperationName,
		Model:          seed.Model,
	})...)

	return callCtx, &GenerationRecorder{
		client:    c,
		ctx:       callCtx,
		span:      span,
		seed:      seed,
		startedAt: startedAt,
	}, nil
}

// StartToolExecution starts an execute_tool span and returns a context for the tool call.
func (c *Client) StartToolExecution(ctx context.Context, start ToolExecutionStart) (context.Context, *ToolExecutionRecorder, error) {
	if c == nil {
		return nil, nil, errors.New("sigil client is nil")
	}

	seed := start
	seed.ToolName = strings.TrimSpace(seed.ToolName)
	if seed.ToolName == "" {
		return nil, nil, errors.New("tool name is required")
	}

	startedAt := seed.StartedAt
	if startedAt.IsZero() {
		startedAt = c.now().UTC()
	} else {
		startedAt = startedAt.UTC()
	}
	seed.StartedAt = startedAt

	callCtx, span := c.startSpan(ctx, Generation{OperationName: "execute_tool", Model: ModelRef{Name: seed.ToolName}}, trace.SpanKindInternal, startedAt)
	attrs := toolSpanAttributes(seed)
	span.SetAttributes(attrs...)

	return callCtx, &ToolExecutionRecorder{
		client:         c,
		ctx:            callCtx,
		span:           span,
		seed:           seed,
		startedAt:      startedAt,
		includeContent: seed.IncludeContent,
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
	r.span.SetAttributes(generationSpanAttributes(generation)...)

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
	if errorType := generationErrorType(callErr, recordErr); errorType != "" {
		r.span.SetAttributes(attribute.String(spanAttrErrorType, errorType))
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

// End finalizes tool execution span attributes, status, and end timestamp.
//
// End is single-use. A second call returns an error and does nothing.
func (r *ToolExecutionRecorder) End(end ToolExecutionEnd, execErr error) error {
	if r == nil {
		return errors.New("tool execution recorder is nil")
	}

	r.mu.Lock()
	if r.ended {
		r.mu.Unlock()
		return errors.New("tool execution recorder already ended")
	}
	r.ended = true
	r.mu.Unlock()
	if r.client == nil || r.span == nil {
		return errors.New("tool execution recorder is not initialized")
	}

	completedAt := end.CompletedAt
	if completedAt.IsZero() {
		completedAt = r.client.now().UTC()
	} else {
		completedAt = completedAt.UTC()
	}

	r.span.SetName(toolSpanName(r.seed.ToolName))
	r.span.SetAttributes(toolSpanAttributes(r.seed)...)

	var contentErr error
	if r.includeContent {
		arguments, err := serializeToolContent(end.Arguments)
		if err != nil {
			contentErr = fmt.Errorf("serialize tool arguments: %w", err)
		} else if arguments != "" {
			r.span.SetAttributes(attribute.String(spanAttrToolCallArguments, arguments))
		}

		result, err := serializeToolContent(end.Result)
		if err != nil && contentErr == nil {
			contentErr = fmt.Errorf("serialize tool result: %w", err)
		} else if err == nil && result != "" {
			r.span.SetAttributes(attribute.String(spanAttrToolCallResult, result))
		}
	}

	var finalErr error
	switch {
	case execErr != nil && contentErr != nil:
		finalErr = errors.Join(execErr, contentErr)
	case execErr != nil:
		finalErr = execErr
	case contentErr != nil:
		finalErr = contentErr
	}
	if finalErr != nil {
		r.span.RecordError(finalErr)
		r.span.SetAttributes(attribute.String(spanAttrErrorType, "tool_execution_error"))
		r.span.SetStatus(codes.Error, finalErr.Error())
	} else {
		r.span.SetStatus(codes.Ok, "")
	}
	r.span.End(trace.WithTimestamp(completedAt))

	return finalErr
}

func (r *GenerationRecorder) normalizeGeneration(raw Generation, completedAt time.Time, callErr error) Generation {
	g := cloneGeneration(raw)

	if g.ID == "" {
		g.ID = r.seed.ID
	}
	if g.ID == "" {
		g.ID = newRandomID("gen")
	}
	if g.ConversationID == "" {
		g.ConversationID = r.seed.ConversationID
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
		return fmt.Errorf("%w: %v", errGenerationValidation, err)
	}

	if _, err := c.externalizeArtifacts(ctx, generation.Artifacts); err != nil {
		return fmt.Errorf("%w: %v", errGenerationStore, err)
	}
	return nil
}

func (c *Client) now() time.Time {
	return c.config.Now()
}

func (c *Client) startSpan(ctx context.Context, generation Generation, kind trace.SpanKind, startedAt time.Time) (context.Context, trace.Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	if kind == 0 {
		kind = trace.SpanKindClient
	}

	opts := []trace.SpanStartOption{
		trace.WithSpanKind(kind),
		trace.WithTimestamp(startedAt),
	}

	return c.config.Tracer.Start(ctx, generationSpanName(generation), opts...)
}

func generationSpanName(g Generation) string {
	operation := strings.TrimSpace(g.OperationName)
	if operation == "" {
		operation = defaultOperationName
	}

	model := strings.TrimSpace(g.Model.Name)
	if model == "" {
		return operation
	}
	return operation + " " + model
}

func generationSpanAttributes(g Generation) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String(spanAttrOperationName, operationName(g)),
	}
	if g.ID != "" {
		attrs = append(attrs, attribute.String(spanAttrGenerationID, g.ID))
	}
	if conversationID := strings.TrimSpace(g.ConversationID); conversationID != "" {
		attrs = append(attrs, attribute.String(spanAttrConversationID, conversationID))
	}
	if provider := strings.TrimSpace(g.Model.Provider); provider != "" {
		attrs = append(attrs, attribute.String(spanAttrProviderName, provider))
	}
	if model := strings.TrimSpace(g.Model.Name); model != "" {
		attrs = append(attrs, attribute.String(spanAttrRequestModel, model))
	}
	if responseID := strings.TrimSpace(g.ResponseID); responseID != "" {
		attrs = append(attrs, attribute.String(spanAttrResponseID, responseID))
	}
	if responseModel := strings.TrimSpace(g.ResponseModel); responseModel != "" {
		attrs = append(attrs, attribute.String(spanAttrResponseModel, responseModel))
	}
	if g.StopReason != "" {
		attrs = append(attrs,
			attribute.StringSlice(spanAttrFinishReasons, []string{g.StopReason}),
		)
	}
	if g.Usage.InputTokens != 0 {
		attrs = append(attrs, attribute.Int64(spanAttrInputTokens, g.Usage.InputTokens))
	}
	if g.Usage.OutputTokens != 0 {
		attrs = append(attrs, attribute.Int64(spanAttrOutputTokens, g.Usage.OutputTokens))
	}
	if g.Usage.CacheReadInputTokens != 0 {
		attrs = append(attrs, attribute.Int64(spanAttrCacheReadTokens, g.Usage.CacheReadInputTokens))
	}
	if g.Usage.CacheWriteInputTokens != 0 {
		attrs = append(attrs, attribute.Int64(spanAttrCacheWriteTokens, g.Usage.CacheWriteInputTokens))
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

func generationErrorType(callErr, recordErr error) string {
	switch {
	case callErr != nil:
		return "provider_call_error"
	case errors.Is(recordErr, errGenerationValidation):
		return "validation_error"
	case errors.Is(recordErr, errGenerationStore):
		return "record_store_error"
	case recordErr != nil:
		return "record_store_error"
	default:
		return ""
	}
}

func toolSpanName(toolName string) string {
	name := strings.TrimSpace(toolName)
	if name == "" {
		name = "unknown"
	}
	return "execute_tool " + name
}

func toolSpanAttributes(start ToolExecutionStart) []attribute.KeyValue {
	attrs := []attribute.KeyValue{
		attribute.String(spanAttrOperationName, "execute_tool"),
		attribute.String(spanAttrToolName, start.ToolName),
	}

	if callID := strings.TrimSpace(start.ToolCallID); callID != "" {
		attrs = append(attrs, attribute.String(spanAttrToolCallID, callID))
	}
	if toolType := strings.TrimSpace(start.ToolType); toolType != "" {
		attrs = append(attrs, attribute.String(spanAttrToolType, toolType))
	}
	if toolDescription := strings.TrimSpace(start.ToolDescription); toolDescription != "" {
		attrs = append(attrs, attribute.String(spanAttrToolDescription, toolDescription))
	}
	if conversationID := strings.TrimSpace(start.ConversationID); conversationID != "" {
		attrs = append(attrs, attribute.String(spanAttrConversationID, conversationID))
	}

	return attrs
}

func serializeToolContent(value any) (string, error) {
	if value == nil {
		return "", nil
	}

	switch v := value.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return "", nil
		}
		if json.Valid([]byte(trimmed)) {
			return trimmed, nil
		}
		data, err := json.Marshal(trimmed)
		if err != nil {
			return "", err
		}
		return string(data), nil
	case []byte:
		trimmed := strings.TrimSpace(string(v))
		if trimmed == "" {
			return "", nil
		}
		if json.Valid([]byte(trimmed)) {
			return trimmed, nil
		}
		data, err := json.Marshal(trimmed)
		if err != nil {
			return "", err
		}
		return string(data), nil
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return "", err
		}
		trimmed := strings.TrimSpace(string(data))
		if trimmed == "null" {
			return "", nil
		}
		return trimmed, nil
	}
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
