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

// Keep unexported aliases for backward-compatible fmt.Errorf wrapping.
var (
	errGenerationValidation = ErrValidationFailed
	errGenerationStore      = ErrStoreFailed
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
//
// The typical usage pattern is:
//
//	ctx, rec := client.StartGeneration(ctx, start)
//	defer rec.End()
//	resp, err := provider.Call(ctx, req)
//	if err != nil { rec.SetCallError(err); return err }
//	rec.SetResult(mapper.FromRequestResponse(req, resp))
//
// All methods are safe to call on a nil or no-op recorder.
type GenerationRecorder struct {
	client    *Client
	ctx       context.Context
	span      trace.Span
	seed      GenerationStart
	startedAt time.Time

	mu             sync.Mutex
	ended          bool
	callErr        error
	mapErr         error
	generation     Generation
	hasResult      bool
	lastGeneration Generation
	finalErr       error
}

// ToolExecutionRecorder records and closes one in-flight execute_tool span.
//
// All methods are safe to call on a nil or no-op recorder.
type ToolExecutionRecorder struct {
	client         *Client
	ctx            context.Context
	span           trace.Span
	seed           ToolExecutionStart
	startedAt      time.Time
	includeContent bool

	mu        sync.Mutex
	ended     bool
	execErr   error
	result    ToolExecutionEnd
	hasResult bool
	finalErr  error
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

// StartGeneration starts a GenAI span and returns a context for the provider call.
//
// Start fields are seeds: End fills zero-valued generation fields from start.
// If the client is nil a no-op recorder is returned (instrumentation never crashes business logic).
//
// Linking is two-way after End:
//   - Generation.TraceID and Generation.SpanID are set from the created span context.
//   - The span includes sigil.generation.id as an attribute.
func (c *Client) StartGeneration(ctx context.Context, start GenerationStart) (context.Context, *GenerationRecorder) {
	if c == nil {
		return ctx, &GenerationRecorder{}
	}

	seed := cloneGenerationStart(start)
	if seed.OperationName == "" {
		seed.OperationName = defaultOperationName
	}
	// Read conversation ID from context when explicit field is empty.
	if seed.ConversationID == "" {
		if id, ok := ConversationIDFromContext(ctx); ok {
			seed.ConversationID = id
		}
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
	}
}

// StartToolExecution starts an execute_tool span and returns a context for the tool call.
// If the client is nil or tool name is empty a no-op recorder is returned.
func (c *Client) StartToolExecution(ctx context.Context, start ToolExecutionStart) (context.Context, *ToolExecutionRecorder) {
	if c == nil {
		return ctx, &ToolExecutionRecorder{}
	}

	seed := start
	seed.ToolName = strings.TrimSpace(seed.ToolName)
	if seed.ToolName == "" {
		return ctx, &ToolExecutionRecorder{}
	}

	// Read conversation ID from context when explicit field is empty.
	if seed.ConversationID == "" {
		if id, ok := ConversationIDFromContext(ctx); ok {
			seed.ConversationID = id
		}
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
	}
}

// ---------------------------------------------------------------------------
// GenerationRecorder builder methods
// ---------------------------------------------------------------------------

// SetCallError records a provider/network call error.
// It is safe to call on a nil recorder.
func (r *GenerationRecorder) SetCallError(err error) {
	if r == nil || err == nil {
		return
	}
	r.mu.Lock()
	r.callErr = err
	r.mu.Unlock()
}

// SetResult stores the mapped generation and/or a mapping error.
// It directly accepts the (Generation, error) return of provider mappers,
// so calls like rec.SetResult(openai.FromRequestResponse(req, resp)) chain naturally.
// It is safe to call on a nil recorder.
func (r *GenerationRecorder) SetResult(g Generation, err error) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.generation = g
	r.mapErr = err
	r.hasResult = true
	r.mu.Unlock()
}

// End finalizes generation recording, sets span status, and closes the span.
//
// End takes no arguments and returns nothing, so it is safe for use with defer:
//
//	ctx, rec := client.StartGeneration(ctx, start)
//	defer rec.End()
//
// End is idempotent; subsequent calls are no-ops.
// It is safe to call on a nil or no-op recorder.
func (r *GenerationRecorder) End() {
	if r == nil {
		return
	}

	r.mu.Lock()
	if r.ended {
		r.mu.Unlock()
		return
	}
	r.ended = true
	callErr := r.callErr
	mapErr := r.mapErr
	generation := r.generation
	r.mu.Unlock()

	// No-op recorder: no client/span means nothing to finalize.
	if r.client == nil || r.span == nil {
		return
	}

	completedAt := r.client.now().UTC()
	normalized := r.normalizeGeneration(generation, completedAt, callErr)
	applyTraceContextFromSpan(r.span, &normalized)

	r.span.SetName(generationSpanName(normalized))
	r.span.SetAttributes(generationSpanAttributes(normalized)...)

	r.mu.Lock()
	r.lastGeneration = cloneGeneration(normalized)
	r.mu.Unlock()

	recordErr := r.client.persistGeneration(r.ctx, normalized)

	// Record errors on span.
	if callErr != nil {
		r.span.RecordError(callErr)
	}
	if mapErr != nil {
		r.span.RecordError(mapErr)
	}
	if recordErr != nil {
		r.span.RecordError(recordErr)
	}

	if errorType := generationErrorType(callErr, mapErr, recordErr); errorType != "" {
		r.span.SetAttributes(attribute.String(spanAttrErrorType, errorType))
	}

	switch {
	case callErr != nil:
		r.span.SetStatus(codes.Error, callErr.Error())
	case mapErr != nil:
		r.span.SetStatus(codes.Error, mapErr.Error())
	case recordErr != nil:
		r.span.SetStatus(codes.Error, recordErr.Error())
	default:
		r.span.SetStatus(codes.Ok, "")
	}
	r.span.End(trace.WithTimestamp(normalized.CompletedAt))

	// Store accumulated error for Err().
	r.mu.Lock()
	r.finalErr = combineAllErrors(callErr, mapErr, recordErr)
	r.mu.Unlock()
}

// Err returns the accumulated error after End has been called, like sql.Rows.Err().
// It is safe to call on a nil recorder.
func (r *GenerationRecorder) Err() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.finalErr
}

// ---------------------------------------------------------------------------
// ToolExecutionRecorder builder methods
// ---------------------------------------------------------------------------

// SetExecError records a tool execution error.
// It is safe to call on a nil recorder.
func (r *ToolExecutionRecorder) SetExecError(err error) {
	if r == nil || err == nil {
		return
	}
	r.mu.Lock()
	r.execErr = err
	r.mu.Unlock()
}

// SetResult stores the tool execution end data.
// It is safe to call on a nil recorder.
func (r *ToolExecutionRecorder) SetResult(end ToolExecutionEnd) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.result = end
	r.hasResult = true
	r.mu.Unlock()
}

// End finalizes tool execution span attributes, status, and end timestamp.
//
// End is idempotent; subsequent calls are no-ops.
// It is safe to call on a nil or no-op recorder.
func (r *ToolExecutionRecorder) End() {
	if r == nil {
		return
	}

	r.mu.Lock()
	if r.ended {
		r.mu.Unlock()
		return
	}
	r.ended = true
	execErr := r.execErr
	end := r.result
	r.mu.Unlock()

	// No-op recorder.
	if r.client == nil || r.span == nil {
		return
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

	r.mu.Lock()
	r.finalErr = finalErr
	r.mu.Unlock()
}

// Err returns the accumulated error after End has been called, like sql.Rows.Err().
// It is safe to call on a nil recorder.
func (r *ToolExecutionRecorder) Err() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.finalErr
}

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

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

func combineAllErrors(callErr, mapErr, recordErr error) error {
	var errs []error
	if callErr != nil {
		errs = append(errs, callErr)
	}
	if mapErr != nil {
		errs = append(errs, fmt.Errorf("mapping: %w", mapErr))
	}
	if recordErr != nil {
		errs = append(errs, fmt.Errorf("record generation: %w", recordErr))
	}
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}
	return errors.Join(errs...)
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

func generationErrorType(callErr, mapErr, recordErr error) string {
	switch {
	case callErr != nil:
		return "provider_call_error"
	case mapErr != nil:
		return "mapping_error"
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
