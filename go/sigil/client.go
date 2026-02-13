package sigil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Config controls Sigil client behavior.
type Config struct {
	Trace            TraceConfig
	GenerationExport GenerationExportConfig
	// Tracer is optional and mainly used for tests. If nil, the client builds one from Trace config.
	Tracer trace.Tracer
	// Logger receives async export failures. Defaults to log.Default().
	Logger *log.Logger
	// Now controls clock behavior (useful for tests).
	Now func() time.Time

	// testGenerationExporter overrides transport for in-package tests.
	testGenerationExporter generationExporter
	// testDisableWorker keeps queue synchronous for in-package tests.
	testDisableWorker bool
}

type TraceProtocol string

const (
	TraceProtocolGRPC TraceProtocol = "grpc"
	TraceProtocolHTTP TraceProtocol = "http"
)

type ExportAuthMode string

const (
	ExportAuthModeNone   ExportAuthMode = "none"
	ExportAuthModeTenant ExportAuthMode = "tenant"
	ExportAuthModeBearer ExportAuthMode = "bearer"
)

type AuthConfig struct {
	Mode        ExportAuthMode
	TenantID    string
	BearerToken string
}

type TraceConfig struct {
	Protocol TraceProtocol
	Endpoint string
	Headers  map[string]string
	Auth     AuthConfig
	Insecure bool
}

type GenerationExportProtocol string

const (
	GenerationExportProtocolGRPC GenerationExportProtocol = "grpc"
	GenerationExportProtocolHTTP GenerationExportProtocol = "http"
)

type GenerationExportConfig struct {
	Protocol        GenerationExportProtocol
	Endpoint        string
	Headers         map[string]string
	Auth            AuthConfig
	Insecure        bool
	BatchSize       int
	FlushInterval   time.Duration
	QueueSize       int
	MaxRetries      int
	InitialBackoff  time.Duration
	MaxBackoff      time.Duration
	PayloadMaxBytes int
}

const instrumentationName = "github.com/grafana/sigil/sdks/go/sigil"
const (
	spanAttrGenerationID           = "sigil.generation.id"
	spanAttrConversationID         = "gen_ai.conversation.id"
	spanAttrAgentName              = "gen_ai.agent.name"
	spanAttrAgentVersion           = "gen_ai.agent.version"
	spanAttrErrorType              = "error.type"
	spanAttrOperationName          = "gen_ai.operation.name"
	spanAttrProviderName           = "gen_ai.provider.name"
	spanAttrRequestModel           = "gen_ai.request.model"
	spanAttrRequestMaxTokens       = "gen_ai.request.max_tokens"
	spanAttrRequestTemperature     = "gen_ai.request.temperature"
	spanAttrRequestTopP            = "gen_ai.request.top_p"
	spanAttrRequestToolChoice      = "sigil.gen_ai.request.tool_choice"
	spanAttrRequestThinkingEnabled = "sigil.gen_ai.request.thinking.enabled"
	spanAttrRequestThinkingBudget  = "sigil.gen_ai.request.thinking.budget_tokens"
	spanAttrResponseID             = "gen_ai.response.id"
	spanAttrResponseModel          = "gen_ai.response.model"
	spanAttrFinishReasons          = "gen_ai.response.finish_reasons"
	spanAttrInputTokens            = "gen_ai.usage.input_tokens"
	spanAttrOutputTokens           = "gen_ai.usage.output_tokens"
	spanAttrCacheReadTokens        = "gen_ai.usage.cache_read_input_tokens"
	spanAttrCacheWriteTokens       = "gen_ai.usage.cache_write_input_tokens"
	spanAttrToolName               = "gen_ai.tool.name"
	spanAttrToolCallID             = "gen_ai.tool.call.id"
	spanAttrToolType               = "gen_ai.tool.type"
	spanAttrToolDescription        = "gen_ai.tool.description"
	spanAttrToolCallArguments      = "gen_ai.tool.call.arguments"
	spanAttrToolCallResult         = "gen_ai.tool.call.result"
)

// Keep unexported aliases for backward-compatible fmt.Errorf wrapping.
var (
	errGenerationValidation = ErrValidationFailed
	errGenerationEnqueue    = ErrEnqueueFailed
)

// DefaultConfig returns a production-ready baseline configuration.
func DefaultConfig() Config {
	return Config{
		Trace: TraceConfig{
			Protocol: TraceProtocolHTTP,
			Endpoint: "http://localhost:4318/v1/traces",
			Auth: AuthConfig{
				Mode: ExportAuthModeNone,
			},
			Insecure: true,
		},
		GenerationExport: GenerationExportConfig{
			Protocol:        GenerationExportProtocolGRPC,
			Endpoint:        "localhost:4317",
			Auth:            AuthConfig{Mode: ExportAuthModeNone},
			Insecure:        true,
			BatchSize:       100,
			FlushInterval:   time.Second,
			QueueSize:       2000,
			MaxRetries:      5,
			InitialBackoff:  100 * time.Millisecond,
			MaxBackoff:      5 * time.Second,
			PayloadMaxBytes: 4 << 20,
		},
		Tracer: nil,
		Logger: log.Default(),
		Now:    time.Now,
	}
}

// Client records normalized generation data and GenAI spans.
type Client struct {
	config        Config
	tracer        trace.Tracer
	traceProvider *sdktrace.TracerProvider
	exporter      generationExporter

	queue    chan queuedGeneration
	flushReq chan chan error

	queueMu      sync.RWMutex
	shutdown     bool
	workerOnce   sync.Once
	shutdownOnce sync.Once
	workerDone   chan struct{}
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
// For streaming calls, use StartStreamingGeneration and set the final stitched
// generation result before End.
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

	cfg.Trace = mergeTraceConfig(defaults.Trace, cfg.Trace)
	cfg.GenerationExport = mergeGenerationExportConfig(defaults.GenerationExport, cfg.GenerationExport)

	traceHeaders, err := resolveHeadersWithAuth(cfg.Trace.Headers, cfg.Trace.Auth)
	if err != nil {
		panic(fmt.Sprintf("invalid trace auth config: %v", err))
	}
	cfg.Trace.Headers = traceHeaders

	generationHeaders, err := resolveHeadersWithAuth(cfg.GenerationExport.Headers, cfg.GenerationExport.Auth)
	if err != nil {
		panic(fmt.Sprintf("invalid generation auth config: %v", err))
	}
	cfg.GenerationExport.Headers = generationHeaders

	if cfg.Now == nil {
		cfg.Now = defaults.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = defaults.Logger
	}

	client := &Client{
		config:     cfg,
		flushReq:   make(chan chan error),
		workerDone: make(chan struct{}),
	}

	if cfg.Tracer != nil {
		client.tracer = cfg.Tracer
	} else {
		tracer, provider, err := newTraceProvider(cfg.Trace)
		if err != nil {
			cfg.Logger.Printf("sigil trace exporter init failed: %v", err)
			client.tracer = defaults.Tracer
		} else {
			client.tracer = tracer
			client.traceProvider = provider
		}
	}

	exporter := cfg.testGenerationExporter
	if exporter == nil {
		var err error
		exporter, err = newGenerationExporter(cfg.GenerationExport)
		if err != nil {
			cfg.Logger.Printf("sigil generation exporter init failed: %v", err)
			exporter = newNoopGenerationExporter(err)
		}
	}
	client.exporter = exporter
	client.queue = make(chan queuedGeneration, cfg.GenerationExport.QueueSize)

	if !cfg.testDisableWorker {
		client.startWorker()
	} else {
		close(client.workerDone)
	}
	return client
}

// StartGeneration starts a non-stream GenAI span and returns a context for the provider call.
//
// Start fields are seeds: End fills zero-valued generation fields from start.
// If the client is nil a no-op recorder is returned (instrumentation never crashes business logic).
//
// Linking is two-way after End:
//   - Generation.TraceID and Generation.SpanID are set from the created span context.
//   - The span includes sigil.generation.id as an attribute.
func (c *Client) StartGeneration(ctx context.Context, start GenerationStart) (context.Context, *GenerationRecorder) {
	return c.startGeneration(ctx, start, GenerationModeSync)
}

// StartStreamingGeneration starts a streaming GenAI span and returns a context for the provider call.
//
// It applies STREAM defaults when start fields are zero-valued.
// If the client is nil a no-op recorder is returned (instrumentation never crashes business logic).
func (c *Client) StartStreamingGeneration(ctx context.Context, start GenerationStart) (context.Context, *GenerationRecorder) {
	return c.startGeneration(ctx, start, GenerationModeStream)
}

func (c *Client) startGeneration(ctx context.Context, start GenerationStart, defaultMode GenerationMode) (context.Context, *GenerationRecorder) {
	if c == nil {
		return ctx, &GenerationRecorder{}
	}

	seed := cloneGenerationStart(start)
	if seed.Mode == "" {
		seed.Mode = defaultMode
	}
	if seed.OperationName == "" {
		seed.OperationName = defaultOperationNameForMode(seed.Mode)
	}
	// Read conversation ID from context when explicit field is empty.
	if seed.ConversationID == "" {
		if id, ok := ConversationIDFromContext(ctx); ok {
			seed.ConversationID = id
		}
	}
	if seed.AgentName == "" {
		if name, ok := AgentNameFromContext(ctx); ok {
			seed.AgentName = name
		}
	}
	if seed.AgentVersion == "" {
		if version, ok := AgentVersionFromContext(ctx); ok {
			seed.AgentVersion = version
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
		ID:              seed.ID,
		ConversationID:  seed.ConversationID,
		AgentName:       seed.AgentName,
		AgentVersion:    seed.AgentVersion,
		Mode:            seed.Mode,
		OperationName:   seed.OperationName,
		Model:           seed.Model,
		MaxTokens:       cloneInt64Ptr(seed.MaxTokens),
		Temperature:     cloneFloat64Ptr(seed.Temperature),
		TopP:            cloneFloat64Ptr(seed.TopP),
		ToolChoice:      cloneStringPtr(seed.ToolChoice),
		ThinkingEnabled: cloneBoolPtr(seed.ThinkingEnabled),
	}, trace.SpanKindClient, startedAt)
	span.SetAttributes(generationSpanAttributes(Generation{
		ID:              seed.ID,
		ConversationID:  seed.ConversationID,
		AgentName:       seed.AgentName,
		AgentVersion:    seed.AgentVersion,
		Mode:            seed.Mode,
		OperationName:   seed.OperationName,
		Model:           seed.Model,
		MaxTokens:       cloneInt64Ptr(seed.MaxTokens),
		Temperature:     cloneFloat64Ptr(seed.Temperature),
		TopP:            cloneFloat64Ptr(seed.TopP),
		ToolChoice:      cloneStringPtr(seed.ToolChoice),
		ThinkingEnabled: cloneBoolPtr(seed.ThinkingEnabled),
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
	if seed.AgentName == "" {
		if name, ok := AgentNameFromContext(ctx); ok {
			seed.AgentName = name
		}
	}
	if seed.AgentVersion == "" {
		if version, ok := AgentVersionFromContext(ctx); ok {
			seed.AgentVersion = version
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

	enqueueErr := r.client.persistGeneration(r.ctx, normalized)

	// Record errors on span.
	if callErr != nil {
		r.span.RecordError(callErr)
	}
	if mapErr != nil {
		r.span.RecordError(mapErr)
	}
	if enqueueErr != nil {
		r.span.RecordError(enqueueErr)
	}

	if errorType := generationErrorType(callErr, mapErr, enqueueErr); errorType != "" {
		r.span.SetAttributes(attribute.String(spanAttrErrorType, errorType))
	}

	switch {
	case callErr != nil:
		r.span.SetStatus(codes.Error, callErr.Error())
	case mapErr != nil:
		r.span.SetStatus(codes.Error, mapErr.Error())
	case enqueueErr != nil:
		r.span.SetStatus(codes.Error, enqueueErr.Error())
	default:
		r.span.SetStatus(codes.Ok, "")
	}
	r.span.End(trace.WithTimestamp(normalized.CompletedAt))

	// rec.Err reports local validation/enqueue failures only.
	r.mu.Lock()
	r.finalErr = combineAllErrors(enqueueErr)
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
	if g.AgentName == "" {
		g.AgentName = r.seed.AgentName
	}
	if g.AgentVersion == "" {
		g.AgentVersion = r.seed.AgentVersion
	}
	if g.Mode == "" {
		g.Mode = r.seed.Mode
	}
	if g.Mode == "" {
		g.Mode = GenerationModeSync
	}
	if g.OperationName == "" {
		g.OperationName = r.seed.OperationName
	}
	if g.OperationName == "" {
		g.OperationName = defaultOperationNameForMode(g.Mode)
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
	if g.MaxTokens == nil {
		g.MaxTokens = cloneInt64Ptr(r.seed.MaxTokens)
	}
	if g.Temperature == nil {
		g.Temperature = cloneFloat64Ptr(r.seed.Temperature)
	}
	if g.TopP == nil {
		g.TopP = cloneFloat64Ptr(r.seed.TopP)
	}
	if g.ToolChoice == nil {
		g.ToolChoice = cloneStringPtr(r.seed.ToolChoice)
	}
	if g.ThinkingEnabled == nil {
		g.ThinkingEnabled = cloneBoolPtr(r.seed.ThinkingEnabled)
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

func combineAllErrors(errs ...error) error {
	filtered := make([]error, 0, len(errs))
	for i := range errs {
		if errs[i] != nil {
			filtered = append(filtered, errs[i])
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	if len(filtered) == 1 {
		return filtered[0]
	}
	return errors.Join(filtered...)
}

func (c *Client) persistGeneration(_ context.Context, generation Generation) error {
	if err := ValidateGeneration(generation); err != nil {
		return fmt.Errorf("%w: %v", errGenerationValidation, err)
	}

	if err := c.enqueueGeneration(generation); err != nil {
		return fmt.Errorf("%w: %w", errGenerationEnqueue, err)
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

	tracer := c.tracer
	if tracer == nil {
		tracer = otel.Tracer(instrumentationName)
	}

	return tracer.Start(ctx, generationSpanName(generation), opts...)
}

func generationSpanName(g Generation) string {
	operation := strings.TrimSpace(g.OperationName)
	if operation == "" {
		operation = defaultOperationNameForMode(g.Mode)
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
	if agentName := strings.TrimSpace(g.AgentName); agentName != "" {
		attrs = append(attrs, attribute.String(spanAttrAgentName, agentName))
	}
	if agentVersion := strings.TrimSpace(g.AgentVersion); agentVersion != "" {
		attrs = append(attrs, attribute.String(spanAttrAgentVersion, agentVersion))
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
	if g.MaxTokens != nil {
		attrs = append(attrs, attribute.Int64(spanAttrRequestMaxTokens, *g.MaxTokens))
	}
	if g.Temperature != nil {
		attrs = append(attrs, attribute.Float64(spanAttrRequestTemperature, *g.Temperature))
	}
	if g.TopP != nil {
		attrs = append(attrs, attribute.Float64(spanAttrRequestTopP, *g.TopP))
	}
	if g.ToolChoice != nil {
		if toolChoice := strings.TrimSpace(*g.ToolChoice); toolChoice != "" {
			attrs = append(attrs, attribute.String(spanAttrRequestToolChoice, toolChoice))
		}
	}
	if g.ThinkingEnabled != nil {
		attrs = append(attrs, attribute.Bool(spanAttrRequestThinkingEnabled, *g.ThinkingEnabled))
	}
	if thinkingBudget, ok := thinkingBudgetFromMetadata(g.Metadata); ok {
		attrs = append(attrs, attribute.Int64(spanAttrRequestThinkingBudget, thinkingBudget))
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
		return defaultOperationNameForMode(g.Mode)
	}

	return operation
}

func thinkingBudgetFromMetadata(metadata map[string]any) (int64, bool) {
	if len(metadata) == 0 {
		return 0, false
	}

	value, ok := metadata[spanAttrRequestThinkingBudget]
	if !ok || value == nil {
		return 0, false
	}

	coerced, ok := coerceInt64(value)
	if !ok {
		return 0, false
	}

	return coerced, true
}

func coerceInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int8:
		return int64(typed), true
	case int16:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case uint:
		return int64(typed), true
	case uint8:
		return int64(typed), true
	case uint16:
		return int64(typed), true
	case uint32:
		return int64(typed), true
	case uint64:
		if typed > uint64(^uint64(0)>>1) {
			return 0, false
		}
		return int64(typed), true
	case float32:
		asInt := int64(typed)
		if float32(asInt) != typed {
			return 0, false
		}
		return asInt, true
	case float64:
		asInt := int64(typed)
		if float64(asInt) != typed {
			return 0, false
		}
		return asInt, true
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func generationErrorType(callErr, mapErr, enqueueErr error) string {
	switch {
	case callErr != nil:
		return "provider_call_error"
	case mapErr != nil:
		return "mapping_error"
	case errors.Is(enqueueErr, errGenerationValidation):
		return "validation_error"
	case errors.Is(enqueueErr, errGenerationEnqueue), errors.Is(enqueueErr, ErrQueueFull):
		return "enqueue_error"
	case enqueueErr != nil:
		return "enqueue_error"
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
	if agentName := strings.TrimSpace(start.AgentName); agentName != "" {
		attrs = append(attrs, attribute.String(spanAttrAgentName, agentName))
	}
	if agentVersion := strings.TrimSpace(start.AgentVersion); agentVersion != "" {
		attrs = append(attrs, attribute.String(spanAttrAgentVersion, agentVersion))
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
