package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	asdk "github.com/anthropics/anthropic-sdk-go"
	goanthropic "github.com/grafana/sigil/sdks/go-providers/anthropic"
	gogemini "github.com/grafana/sigil/sdks/go-providers/gemini"
	goopenai "github.com/grafana/sigil/sdks/go-providers/openai"
	"github.com/grafana/sigil/sdks/go/sigil"
	osdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	oresponses "github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
	"google.golang.org/genai"
)

const (
	languageName        = "go"
	traceServiceName    = "sigil-sdk-traffic-go"
	traceServiceEnv     = "sigil-devex"
	traceShutdownGrace  = 5 * time.Second
	metricFlushInterval = 2 * time.Second
	minSyntheticSpans   = 15
	maxSyntheticSpans   = 30
)

type runtimeConfig struct {
	interval       time.Duration
	streamPercent  int
	conversations  int
	rotateTurns    int
	maxCycles      int
	customProvider string
	genGRPC        string
	traceGRPC      string
}

type source string

const (
	sourceOpenAI    source = "openai"
	sourceAnthropic source = "anthropic"
	sourceGemini    source = "gemini"
	sourceCustom    source = "mistral"
)

type threadState struct {
	conversationID string
	turn           int
}

type tagEnvelope struct {
	agentPersona string
	tags         map[string]string
	metadata     map[string]any
}

func main() {
	cfg := loadConfig()
	randSeed := rand.New(rand.NewSource(time.Now().UnixNano()))
	telemetryShutdown, err := configureTelemetry(context.Background(), cfg)
	if err != nil {
		log.Fatalf("[go-emitter] telemetry setup failed: %v", err)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), traceShutdownGrace)
		defer cancel()
		if err := telemetryShutdown(shutdownCtx); err != nil {
			log.Printf("[go-emitter] telemetry shutdown error: %v", err)
		}
	}()

	clientCfg := sigil.DefaultConfig()
	clientCfg.GenerationExport.Protocol = sigil.GenerationExportProtocolGRPC
	clientCfg.GenerationExport.Endpoint = cfg.genGRPC
	clientCfg.GenerationExport.Auth = sigil.AuthConfig{Mode: sigil.ExportAuthModeNone}

	client := sigil.NewClient(clientCfg)
	defer func() {
		if err := client.Shutdown(context.Background()); err != nil {
			log.Printf("[go-emitter] shutdown error: %v", err)
		}
	}()

	sources := []source{sourceOpenAI, sourceAnthropic, sourceGemini, sourceCustom}
	threads := make(map[source][]threadState, len(sources))
	nextSlot := make(map[source]int, len(sources))
	for _, src := range sources {
		threads[src] = make([]threadState, cfg.conversations)
	}

	log.Printf(
		"[go-emitter] started interval=%s stream_percent=%d conversations=%d rotate_turns=%d custom_provider=%s trace_grpc=%s",
		cfg.interval,
		cfg.streamPercent,
		cfg.conversations,
		cfg.rotateTurns,
		cfg.customProvider,
		cfg.traceGRPC,
	)
	cycles := 0

	for {
		for _, src := range sources {
			slot := nextSlot[src] % cfg.conversations
			nextSlot[src]++

			thread := &threads[src][slot]
			ensureThread(thread, cfg.rotateTurns, src, slot)
			mode := chooseMode(randSeed.Intn(100), cfg.streamPercent)

			if err := emitForSource(client, cfg, randSeed, src, slot, thread, mode); err != nil {
				log.Fatalf("[go-emitter] emit failed source=%s slot=%d turn=%d: %v", src, slot, thread.turn, err)
			}
			thread.turn++
		}

		cycles++
		if cfg.maxCycles > 0 && cycles >= cfg.maxCycles {
			return
		}

		jitterMs := randSeed.Intn(401) - 200
		sleep := cfg.interval + time.Duration(jitterMs)*time.Millisecond
		if sleep < 200*time.Millisecond {
			sleep = 200 * time.Millisecond
		}
		time.Sleep(sleep)
	}
}

func emitForSource(client *sigil.Client, cfg runtimeConfig, randSeed *rand.Rand, src source, slot int, thread *threadState, mode sigil.GenerationMode) error {
	envelope := buildTagEnvelope(src, mode, thread.turn, slot)
	agentName := fmt.Sprintf("devex-%s-%s-%s", languageName, src, envelope.agentPersona)
	agentVersion := "devex-1"

	ctx := context.Background()
	tracer := otel.Tracer("sigil.devex.synthetic")
	ctx, conversationSpan := tracer.Start(
		ctx,
		fmt.Sprintf("conversation.%s.turn", src),
		oteltrace.WithAttributes(
			attribute.String("sigil.synthetic.trace_type", "llm_conversation"),
			attribute.String("sigil.devex.provider", string(src)),
			attribute.String("sigil.devex.mode", string(mode)),
			attribute.String("sigil.devex.conversation_id", thread.conversationID),
			attribute.Int("sigil.devex.turn", thread.turn),
			attribute.Int("sigil.devex.slot", slot),
			attribute.String("sigil.devex.scenario", envelope.tags["sigil.devex.scenario"]),
		),
	)
	defer conversationSpan.End()
	syntheticCount := emitSyntheticLifecycleSpans(ctx, randSeed)
	conversationSpan.SetAttributes(attribute.Int("sigil.synthetic.span_count", syntheticCount))

	switch src {
	case sourceOpenAI:
		if mode == sigil.GenerationModeStream {
			if openAIUsesResponses(thread.turn) {
				return emitOpenAIResponsesStream(ctx, client, thread.conversationID, agentName, agentVersion, envelope.tags, envelope.metadata, thread.turn)
			}
			return emitOpenAIChatCompletionsStream(ctx, client, thread.conversationID, agentName, agentVersion, envelope.tags, envelope.metadata, thread.turn)
		}
		if openAIUsesResponses(thread.turn) {
			return emitOpenAIResponsesSync(ctx, client, thread.conversationID, agentName, agentVersion, envelope.tags, envelope.metadata, thread.turn)
		}
		return emitOpenAIChatCompletionsSync(ctx, client, thread.conversationID, agentName, agentVersion, envelope.tags, envelope.metadata, thread.turn)
	case sourceAnthropic:
		if mode == sigil.GenerationModeStream {
			return emitAnthropicStream(ctx, client, thread.conversationID, agentName, agentVersion, envelope.tags, envelope.metadata, thread.turn)
		}
		return emitAnthropicSync(ctx, client, thread.conversationID, agentName, agentVersion, envelope.tags, envelope.metadata, thread.turn)
	case sourceGemini:
		if mode == sigil.GenerationModeStream {
			return emitGeminiStream(ctx, client, thread.conversationID, agentName, agentVersion, envelope.tags, envelope.metadata, thread.turn)
		}
		return emitGeminiSync(ctx, client, thread.conversationID, agentName, agentVersion, envelope.tags, envelope.metadata, thread.turn)
	case sourceCustom:
		provider := cfg.customProvider
		if provider == "" {
			provider = string(sourceCustom)
		}
		if mode == sigil.GenerationModeStream {
			return emitCustomStream(ctx, client, provider, thread.conversationID, agentName, agentVersion, envelope.tags, envelope.metadata, thread.turn, randSeed)
		}
		return emitCustomSync(ctx, client, provider, thread.conversationID, agentName, agentVersion, envelope.tags, envelope.metadata, thread.turn, randSeed)
	default:
		return fmt.Errorf("unknown source %q", src)
	}
}

func emitSyntheticLifecycleSpans(ctx context.Context, randSeed *rand.Rand) int {
	if randSeed == nil {
		randSeed = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	operations := []struct {
		name      string
		category  string
		component string
	}{
		{name: "auth.validate_session", category: "auth", component: "auth-service"},
		{name: "auth.refresh_token", category: "auth", component: "auth-service"},
		{name: "db.load_conversation_context", category: "database", component: "postgres"},
		{name: "db.store_generation_metadata", category: "database", component: "postgres"},
		{name: "cache.redis_get", category: "cache", component: "redis"},
		{name: "cache.redis_set", category: "cache", component: "redis"},
		{name: "retrieval.vector_search", category: "retrieval", component: "vector-db"},
		{name: "retrieval.rerank_documents", category: "retrieval", component: "reranker"},
		{name: "tools.web_search.call", category: "tool_call", component: "tool-runner"},
		{name: "tools.sql_query.call", category: "tool_call", component: "tool-runner"},
		{name: "tools.code_interpreter.call", category: "tool_call", component: "tool-runner"},
		{name: "policy.safety_screen", category: "guardrail", component: "safety-service"},
		{name: "prompt.assemble_context", category: "prompting", component: "prompt-builder"},
		{name: "llm.request", category: "model", component: "provider-gateway"},
		{name: "llm.first_token_wait", category: "model", component: "provider-gateway"},
		{name: "output.stream_chunks", category: "streaming", component: "stream-router"},
		{name: "external.crm_lookup", category: "external_service", component: "crm-api"},
		{name: "external.calendar_lookup", category: "external_service", component: "calendar-api"},
		{name: "external.slack_post", category: "external_service", component: "slack-api"},
		{name: "observability.emit_metrics", category: "telemetry", component: "metrics-pipeline"},
	}

	spanCount := minSyntheticSpans + randSeed.Intn(maxSyntheticSpans-minSyntheticSpans+1)
	tracer := otel.Tracer("sigil.devex.synthetic")

	for i := 0; i < spanCount; i++ {
		op := operations[randSeed.Intn(len(operations))]
		duration := syntheticDuration(op.category, randSeed)
		endTime := time.Now()
		startTime := endTime.Add(-duration)

		_, span := tracer.Start(
			ctx,
			op.name,
			oteltrace.WithTimestamp(startTime),
			oteltrace.WithAttributes(
				attribute.String("sigil.synthetic.category", op.category),
				attribute.String("sigil.synthetic.component", op.component),
				attribute.Int("sigil.synthetic.step_index", i),
				attribute.Int64("sigil.synthetic.simulated_duration_ms", duration.Milliseconds()),
			),
		)

		if op.category == "database" {
			span.SetAttributes(
				attribute.String("db.system", "postgresql"),
				attribute.String("db.operation", []string{"SELECT", "INSERT", "UPDATE"}[randSeed.Intn(3)]),
			)
		}
		if op.category == "tool_call" {
			toolNames := []string{"web_search", "sql_query", "code_interpreter", "ticket_lookup"}
			span.SetAttributes(attribute.String("gen_ai.tool.name", toolNames[randSeed.Intn(len(toolNames))]))
		}
		if op.category == "external_service" {
			host := []string{"crm.internal", "calendar.internal", "slack.com"}[randSeed.Intn(3)]
			span.SetAttributes(attribute.String("server.address", host))
		}
		if op.category == "model" {
			span.SetAttributes(
				attribute.String("gen_ai.operation.name", []string{"generateText", "streamText"}[randSeed.Intn(2)]),
				attribute.String("gen_ai.request.model", []string{"gpt-5", "claude-sonnet-4-5", "gemini-2.5-pro"}[randSeed.Intn(3)]),
			)
		}

		// Keep failures sparse but present so UI/testing can exercise error states.
		if randSeed.Intn(100) < 12 {
			errorType := []string{"timeout", "rate_limit", "upstream_503", "validation_error"}[randSeed.Intn(4)]
			span.SetStatus(codes.Error, errorType)
			span.SetAttributes(
				attribute.String("error.type", errorType),
				attribute.Bool("error", true),
			)
		}

		span.End(oteltrace.WithTimestamp(endTime))
	}

	return spanCount
}

func syntheticDuration(category string, randSeed *rand.Rand) time.Duration {
	switch category {
	case "auth":
		return time.Duration(8+randSeed.Intn(24)) * time.Millisecond
	case "database":
		return time.Duration(18+randSeed.Intn(120)) * time.Millisecond
	case "cache":
		return time.Duration(2+randSeed.Intn(10)) * time.Millisecond
	case "retrieval":
		return time.Duration(25+randSeed.Intn(150)) * time.Millisecond
	case "tool_call":
		return time.Duration(45+randSeed.Intn(260)) * time.Millisecond
	case "guardrail":
		return time.Duration(15+randSeed.Intn(70)) * time.Millisecond
	case "prompting":
		return time.Duration(10+randSeed.Intn(40)) * time.Millisecond
	case "model":
		return time.Duration(90+randSeed.Intn(520)) * time.Millisecond
	case "streaming":
		return time.Duration(25+randSeed.Intn(130)) * time.Millisecond
	case "external_service":
		return time.Duration(40+randSeed.Intn(220)) * time.Millisecond
	default:
		return time.Duration(10+randSeed.Intn(100)) * time.Millisecond
	}
}

func emitOpenAIChatCompletionsSync(
	ctx context.Context,
	client *sigil.Client,
	conversationID string,
	agentName string,
	agentVersion string,
	tags map[string]string,
	metadata map[string]any,
	turn int,
) error {
	req := osdk.ChatCompletionNewParams{
		Model: shared.ChatModel("gpt-5"),
		Messages: []osdk.ChatCompletionMessageParamUnion{
			osdk.SystemMessage("You are a concise planner that always returns action bullets."),
			osdk.UserMessage(fmt.Sprintf("Plan run %d for shipping issue triage.", turn)),
		},
	}
	resp := &osdk.ChatCompletion{
		ID:    fmt.Sprintf("go-openai-sync-%d", turn),
		Model: "gpt-5",
		Choices: []osdk.ChatCompletionChoice{
			{
				FinishReason: "stop",
				Message: osdk.ChatCompletionMessage{
					Content: "1. Pull recent incidents\n2. Group by owner\n3. Draft next action",
				},
			},
		},
		Usage: osdk.CompletionUsage{
			PromptTokens:     int64(80 + turn%15),
			CompletionTokens: int64(28 + turn%9),
			TotalTokens:      int64(108 + turn%24),
		},
	}

	mapped, err := goopenai.ChatCompletionsFromRequestResponse(req, resp,
		goopenai.WithConversationID(conversationID),
		goopenai.WithAgentName(agentName),
		goopenai.WithAgentVersion(agentVersion),
		goopenai.WithTags(tags),
		goopenai.WithMetadata(metadata),
	)
	if err != nil {
		return err
	}

	_, rec := client.StartGeneration(ctx, sigil.GenerationStart{Model: sigil.ModelRef{Provider: "openai", Name: "gpt-5"}})
	rec.SetResult(mapped, nil)
	rec.End()
	return rec.Err()
}

func emitOpenAIChatCompletionsStream(
	ctx context.Context,
	client *sigil.Client,
	conversationID string,
	agentName string,
	agentVersion string,
	tags map[string]string,
	metadata map[string]any,
	turn int,
) error {
	req := osdk.ChatCompletionNewParams{
		Model: shared.ChatModel("gpt-5"),
		Messages: []osdk.ChatCompletionMessageParamUnion{
			osdk.UserMessage(fmt.Sprintf("Stream an execution status update for ticket %d.", turn)),
		},
	}
	summary := goopenai.ChatCompletionsStreamSummary{
		Chunks: []osdk.ChatCompletionChunk{
			{
				ID:    fmt.Sprintf("go-openai-stream-%d", turn),
				Model: "gpt-5",
				Choices: []osdk.ChatCompletionChunkChoice{
					{
						Delta: osdk.ChatCompletionChunkChoiceDelta{Content: "Starting rollout checks..."},
					},
					{
						Delta:        osdk.ChatCompletionChunkChoiceDelta{Content: " completed."},
						FinishReason: "stop",
					},
				},
				Usage: osdk.CompletionUsage{
					PromptTokens:     42,
					CompletionTokens: 14,
					TotalTokens:      56,
				},
			},
		},
	}

	mapped, err := goopenai.ChatCompletionsFromStream(req, summary,
		goopenai.WithConversationID(conversationID),
		goopenai.WithAgentName(agentName),
		goopenai.WithAgentVersion(agentVersion),
		goopenai.WithTags(tags),
		goopenai.WithMetadata(metadata),
	)
	if err != nil {
		return err
	}

	_, rec := client.StartStreamingGeneration(ctx, sigil.GenerationStart{Model: sigil.ModelRef{Provider: "openai", Name: "gpt-5"}})
	rec.SetFirstTokenAt(time.Now().UTC())
	rec.SetResult(mapped, nil)
	rec.End()
	return rec.Err()
}

func emitOpenAIResponsesSync(
	ctx context.Context,
	client *sigil.Client,
	conversationID string,
	agentName string,
	agentVersion string,
	tags map[string]string,
	metadata map[string]any,
	turn int,
) error {
	req := oresponses.ResponseNewParams{
		Model:           shared.ResponsesModel("gpt-5"),
		Instructions:    param.NewOpt("You are a concise planner that always returns action bullets."),
		Input:           oresponses.ResponseNewParamsInputUnion{OfString: param.NewOpt(fmt.Sprintf("Plan run %d for shipping issue triage.", turn))},
		MaxOutputTokens: param.NewOpt(int64(256)),
	}
	resp := &oresponses.Response{
		ID:     fmt.Sprintf("go-openai-responses-sync-%d", turn),
		Model:  shared.ResponsesModel("gpt-5"),
		Status: oresponses.ResponseStatusCompleted,
		Output: []oresponses.ResponseOutputItemUnion{
			{
				Type: "message",
				Content: []oresponses.ResponseOutputMessageContentUnion{
					{Type: "output_text", Text: "1. Pull recent incidents\n2. Group by owner\n3. Draft next action"},
				},
			},
		},
		Usage: oresponses.ResponseUsage{
			InputTokens:  int64(80 + turn%15),
			OutputTokens: int64(28 + turn%9),
			TotalTokens:  int64(108 + turn%24),
		},
	}

	mapped, err := goopenai.ResponsesFromRequestResponse(req, resp,
		goopenai.WithConversationID(conversationID),
		goopenai.WithAgentName(agentName),
		goopenai.WithAgentVersion(agentVersion),
		goopenai.WithTags(tags),
		goopenai.WithMetadata(metadata),
	)
	if err != nil {
		return err
	}

	_, rec := client.StartGeneration(ctx, sigil.GenerationStart{Model: sigil.ModelRef{Provider: "openai", Name: "gpt-5"}})
	rec.SetResult(mapped, nil)
	rec.End()
	return rec.Err()
}

func emitOpenAIResponsesStream(
	ctx context.Context,
	client *sigil.Client,
	conversationID string,
	agentName string,
	agentVersion string,
	tags map[string]string,
	metadata map[string]any,
	turn int,
) error {
	req := oresponses.ResponseNewParams{
		Model: shared.ResponsesModel("gpt-5"),
		Input: oresponses.ResponseNewParamsInputUnion{
			OfString: param.NewOpt(fmt.Sprintf("Stream an execution status update for ticket %d.", turn)),
		},
		MaxOutputTokens: param.NewOpt(int64(128)),
	}

	summary := goopenai.ResponsesStreamSummary{
		Events: []oresponses.ResponseStreamEventUnion{
			{
				Type:  "response.output_text.delta",
				Delta: "Starting rollout checks...",
			},
			{
				Type:  "response.output_text.delta",
				Delta: " completed.",
			},
			{
				Type: "response.completed",
				Response: oresponses.Response{
					ID:     fmt.Sprintf("go-openai-responses-stream-%d", turn),
					Model:  shared.ResponsesModel("gpt-5"),
					Status: oresponses.ResponseStatusCompleted,
					Usage: oresponses.ResponseUsage{
						InputTokens:  42,
						OutputTokens: 14,
						TotalTokens:  56,
					},
				},
			},
		},
	}

	mapped, err := goopenai.ResponsesFromStream(req, summary,
		goopenai.WithConversationID(conversationID),
		goopenai.WithAgentName(agentName),
		goopenai.WithAgentVersion(agentVersion),
		goopenai.WithTags(tags),
		goopenai.WithMetadata(metadata),
	)
	if err != nil {
		return err
	}

	_, rec := client.StartStreamingGeneration(ctx, sigil.GenerationStart{Model: sigil.ModelRef{Provider: "openai", Name: "gpt-5"}})
	rec.SetFirstTokenAt(time.Now().UTC())
	rec.SetResult(mapped, nil)
	rec.End()
	return rec.Err()
}

func emitAnthropicSync(
	ctx context.Context,
	client *sigil.Client,
	conversationID string,
	agentName string,
	agentVersion string,
	tags map[string]string,
	metadata map[string]any,
	turn int,
) error {
	req := asdk.BetaMessageNewParams{
		Model: asdk.Model("claude-sonnet-4-5"),
		System: []asdk.BetaTextBlockParam{{
			Text: "Think in short phases and include rationale.",
		}},
		Messages: []asdk.BetaMessageParam{{
			Role: asdk.BetaMessageParamRoleUser,
			Content: []asdk.BetaContentBlockParamUnion{
				asdk.NewBetaTextBlock(fmt.Sprintf("Summarize weekly reliability drift (%d).", turn)),
			},
		}},
	}
	resp := &asdk.BetaMessage{
		ID:    fmt.Sprintf("go-anthropic-sync-%d", turn),
		Model: asdk.Model("claude-sonnet-4-5"),
		Content: []asdk.BetaContentBlockUnion{
			{Type: "thinking", Thinking: "identify top two drift vectors"},
			{Type: "text", Text: "Drift rose in retries and latency on EU shards."},
		},
		StopReason: asdk.BetaStopReasonEndTurn,
		Usage: asdk.BetaUsage{
			InputTokens:  75,
			OutputTokens: 31,
		},
	}

	mapped, err := goanthropic.FromRequestResponse(req, resp,
		goanthropic.WithConversationID(conversationID),
		goanthropic.WithAgentName(agentName),
		goanthropic.WithAgentVersion(agentVersion),
		goanthropic.WithTags(tags),
		goanthropic.WithMetadata(metadata),
	)
	if err != nil {
		return err
	}

	_, rec := client.StartGeneration(ctx, sigil.GenerationStart{Model: sigil.ModelRef{Provider: "anthropic", Name: "claude-sonnet-4-5"}})
	rec.SetResult(mapped, nil)
	rec.End()
	return rec.Err()
}

func emitAnthropicStream(
	ctx context.Context,
	client *sigil.Client,
	conversationID string,
	agentName string,
	agentVersion string,
	tags map[string]string,
	metadata map[string]any,
	turn int,
) error {
	req := asdk.BetaMessageNewParams{
		Model: asdk.Model("claude-sonnet-4-5"),
		Messages: []asdk.BetaMessageParam{{
			Role: asdk.BetaMessageParamRoleUser,
			Content: []asdk.BetaContentBlockParamUnion{
				asdk.NewBetaTextBlock(fmt.Sprintf("Stream a live mitigation status for deployment %d.", turn)),
			},
		}},
	}

	summary := goanthropic.StreamSummary{
		Events: []asdk.BetaRawMessageStreamEventUnion{
			{
				Type: "message_start",
				Message: asdk.BetaMessage{
					ID:    fmt.Sprintf("go-anthropic-stream-%d", turn),
					Model: asdk.Model("claude-sonnet-4-5"),
				},
			},
			{
				Type: "content_block_start",
				ContentBlock: asdk.BetaRawContentBlockStartEventContentBlockUnion{
					Type: "text",
					Text: "Mitigation running on canary set.",
				},
			},
			{
				Type: "message_delta",
				Delta: asdk.BetaRawMessageStreamEventUnionDelta{
					StopReason: asdk.BetaStopReasonEndTurn,
				},
			},
		},
	}

	mapped, err := goanthropic.FromStream(req, summary,
		goanthropic.WithConversationID(conversationID),
		goanthropic.WithAgentName(agentName),
		goanthropic.WithAgentVersion(agentVersion),
		goanthropic.WithTags(tags),
		goanthropic.WithMetadata(metadata),
	)
	if err != nil {
		return err
	}

	_, rec := client.StartStreamingGeneration(ctx, sigil.GenerationStart{Model: sigil.ModelRef{Provider: "anthropic", Name: "claude-sonnet-4-5"}})
	rec.SetFirstTokenAt(time.Now().UTC())
	rec.SetResult(mapped, nil)
	rec.End()
	return rec.Err()
}

func emitGeminiSync(
	ctx context.Context,
	client *sigil.Client,
	conversationID string,
	agentName string,
	agentVersion string,
	tags map[string]string,
	metadata map[string]any,
	turn int,
) error {
	model := "gemini-2.5-pro"
	contents := []*genai.Content{
		genai.NewContentFromText(fmt.Sprintf("Draft a short launch note for sprint %d.", turn), genai.RoleUser),
	}
	var requestConfig *genai.GenerateContentConfig
	resp := &genai.GenerateContentResponse{
		ResponseID:   fmt.Sprintf("go-gemini-sync-%d", turn),
		ModelVersion: "gemini-2.5-pro-001",
		Candidates: []*genai.Candidate{
			{
				FinishReason: genai.FinishReasonStop,
				Content:      genai.NewContentFromText("Launch note: rollout green, no regressions observed.", genai.RoleModel),
			},
		},
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     54,
			CandidatesTokenCount: 18,
			TotalTokenCount:      72,
		},
	}

	mapped, err := gogemini.FromRequestResponse(model, contents, requestConfig, resp,
		gogemini.WithConversationID(conversationID),
		gogemini.WithAgentName(agentName),
		gogemini.WithAgentVersion(agentVersion),
		gogemini.WithTags(tags),
		gogemini.WithMetadata(metadata),
	)
	if err != nil {
		return err
	}

	_, rec := client.StartGeneration(ctx, sigil.GenerationStart{Model: sigil.ModelRef{Provider: "gemini", Name: "gemini-2.5-pro"}})
	rec.SetResult(mapped, nil)
	rec.End()
	return rec.Err()
}

func emitGeminiStream(
	ctx context.Context,
	client *sigil.Client,
	conversationID string,
	agentName string,
	agentVersion string,
	tags map[string]string,
	metadata map[string]any,
	turn int,
) error {
	model := "gemini-2.5-pro"
	contents := []*genai.Content{
		genai.NewContentFromText(fmt.Sprintf("Stream a migration checklist status for wave %d.", turn), genai.RoleUser),
	}
	var requestConfig *genai.GenerateContentConfig
	summary := gogemini.StreamSummary{
		Responses: []*genai.GenerateContentResponse{
			{
				ResponseID:   fmt.Sprintf("go-gemini-stream-%d", turn),
				ModelVersion: "gemini-2.5-pro-001",
				Candidates: []*genai.Candidate{
					{
						Content:      genai.NewContentFromText("Checklist in progress...", genai.RoleModel),
						FinishReason: genai.FinishReasonUnspecified,
					},
				},
			},
			{
				ResponseID:   fmt.Sprintf("go-gemini-stream-%d", turn),
				ModelVersion: "gemini-2.5-pro-001",
				Candidates: []*genai.Candidate{
					{
						Content:      genai.NewContentFromText("Checklist complete. All gates passed.", genai.RoleModel),
						FinishReason: genai.FinishReasonStop,
					},
				},
				UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
					PromptTokenCount:     44,
					CandidatesTokenCount: 16,
					TotalTokenCount:      60,
				},
			},
		},
	}

	mapped, err := gogemini.FromStream(model, contents, requestConfig, summary,
		gogemini.WithConversationID(conversationID),
		gogemini.WithAgentName(agentName),
		gogemini.WithAgentVersion(agentVersion),
		gogemini.WithTags(tags),
		gogemini.WithMetadata(metadata),
	)
	if err != nil {
		return err
	}

	_, rec := client.StartStreamingGeneration(ctx, sigil.GenerationStart{Model: sigil.ModelRef{Provider: "gemini", Name: "gemini-2.5-pro"}})
	rec.SetFirstTokenAt(time.Now().UTC())
	rec.SetResult(mapped, nil)
	rec.End()
	return rec.Err()
}

func emitCustomSync(
	ctx context.Context,
	client *sigil.Client,
	provider string,
	conversationID string,
	agentName string,
	agentVersion string,
	tags map[string]string,
	metadata map[string]any,
	turn int,
	randSeed *rand.Rand,
) error {
	_, rec := client.StartGeneration(ctx, sigil.GenerationStart{
		ConversationID: conversationID,
		AgentName:      agentName,
		AgentVersion:   agentVersion,
		Model: sigil.ModelRef{
			Provider: provider,
			Name:     "mistral-large-devex",
		},
		Tags:     tags,
		Metadata: metadata,
	})
	result := sigil.Generation{
		Input: []sigil.Message{
			sigil.UserTextMessage(fmt.Sprintf("Generate custom provider narrative for checkpoint %d.", turn)),
		},
		Output: []sigil.Message{
			sigil.AssistantTextMessage("Custom provider: checkpoint healthy, drift below threshold."),
		},
		Usage: sigil.TokenUsage{
			InputTokens:  int64(30 + randSeed.Intn(10)),
			OutputTokens: int64(16 + randSeed.Intn(6)),
		},
		StopReason: "stop",
	}
	rec.SetResult(result, nil)
	rec.End()
	return rec.Err()
}

func emitCustomStream(
	ctx context.Context,
	client *sigil.Client,
	provider string,
	conversationID string,
	agentName string,
	agentVersion string,
	tags map[string]string,
	metadata map[string]any,
	turn int,
	randSeed *rand.Rand,
) error {
	_, rec := client.StartStreamingGeneration(ctx, sigil.GenerationStart{
		ConversationID: conversationID,
		AgentName:      agentName,
		AgentVersion:   agentVersion,
		Model: sigil.ModelRef{
			Provider: provider,
			Name:     "mistral-large-devex",
		},
		Tags:     tags,
		Metadata: metadata,
	})
	rec.SetFirstTokenAt(time.Now().UTC())

	result := sigil.Generation{
		Input: []sigil.Message{
			sigil.UserTextMessage(fmt.Sprintf("Stream a custom remediation summary for slot %d turn %d.", metadata["conversation_slot"], turn)),
		},
		Output: []sigil.Message{
			{
				Role: sigil.RoleAssistant,
				Parts: []sigil.Part{
					sigil.ThinkingPart("composing synthetic stream segments"),
					sigil.TextPart("Segment A complete. Segment B complete."),
				},
			},
		},
		Usage: sigil.TokenUsage{
			InputTokens:  int64(26 + randSeed.Intn(12)),
			OutputTokens: int64(18 + randSeed.Intn(7)),
		},
		StopReason: "end_turn",
	}
	rec.SetResult(result, nil)
	rec.End()
	return rec.Err()
}

func sourceTagFor(src source) string {
	if src == sourceCustom {
		return "core_custom"
	}
	return "provider_wrapper"
}

func ensureThread(thread *threadState, rotateTurns int, src source, slot int) {
	if thread.conversationID == "" || thread.turn >= rotateTurns {
		thread.conversationID = newConversationID(languageName, string(src), slot)
		thread.turn = 0
	}
}

func chooseMode(roll int, streamPercent int) sigil.GenerationMode {
	if roll < streamPercent {
		return sigil.GenerationModeStream
	}
	return sigil.GenerationModeSync
}

func buildTagEnvelope(src source, mode sigil.GenerationMode, turn int, slot int) tagEnvelope {
	agentPersona := personaForTurn(turn)
	return tagEnvelope{
		agentPersona: agentPersona,
		tags: map[string]string{
			"sigil.devex.language": languageName,
			"sigil.devex.provider": string(src),
			"sigil.devex.source":   sourceTagFor(src),
			"sigil.devex.scenario": scenarioFor(src, turn),
			"sigil.devex.mode":     string(mode),
		},
		metadata: map[string]any{
			"turn_index":        turn,
			"conversation_slot": slot,
			"agent_persona":     agentPersona,
			"emitter":           "sdk-traffic",
			"provider_shape":    providerShapeFor(src, turn),
		},
	}
}

func scenarioFor(src source, turn int) string {
	switch src {
	case sourceOpenAI:
		if turn%2 == 0 {
			return "planning_brief"
		}
		return "status_stream"
	case sourceAnthropic:
		if turn%2 == 0 {
			return "reasoning_digest"
		}
		return "delta_stream"
	case sourceGemini:
		if turn%2 == 0 {
			return "launch_note"
		}
		return "checklist_stream"
	default:
		if turn%2 == 0 {
			return "custom_sync"
		}
		return "custom_stream"
	}
}

func providerShapeFor(src source, turn int) string {
	switch src {
	case sourceOpenAI:
		if openAIUsesResponses(turn) {
			return "openai_responses"
		}
		return "openai_chat_completions"
	case sourceAnthropic:
		return "messages"
	case sourceGemini:
		return "generate_content"
	default:
		return "core_generation"
	}
}

func openAIUsesResponses(turn int) bool {
	return turn%2 != 0
}

func personaForTurn(turn int) string {
	personas := []string{"planner", "retriever", "executor"}
	return personas[turn%len(personas)]
}

func newConversationID(language, provider string, slot int) string {
	return fmt.Sprintf("devex-%s-%s-%d-%d", language, provider, slot, time.Now().UnixMilli())
}

func loadConfig() runtimeConfig {
	return runtimeConfig{
		interval:       time.Duration(intFromEnv("SIGIL_TRAFFIC_INTERVAL_MS", 2000)) * time.Millisecond,
		streamPercent:  intFromEnv("SIGIL_TRAFFIC_STREAM_PERCENT", 30),
		conversations:  intFromEnv("SIGIL_TRAFFIC_CONVERSATIONS", 3),
		rotateTurns:    intFromEnv("SIGIL_TRAFFIC_ROTATE_TURNS", 24),
		maxCycles:      intFromEnv("SIGIL_TRAFFIC_MAX_CYCLES", 0),
		customProvider: strings.TrimSpace(stringFromEnv("SIGIL_TRAFFIC_CUSTOM_PROVIDER", "mistral")),
		genGRPC:        stringFromEnv("SIGIL_TRAFFIC_GEN_GRPC_ENDPOINT", "sigil:4317"),
		traceGRPC:      stringFromEnv("SIGIL_TRAFFIC_TRACE_GRPC_ENDPOINT", "alloy:4317"),
	}
}

func configureTelemetry(ctx context.Context, cfg runtimeConfig) (func(context.Context) error, error) {
	telemetryEndpoint := strings.TrimSpace(cfg.traceGRPC)
	if telemetryEndpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	traceExporter, err := otlptracegrpc.New(
		ctx,
		otlptracegrpc.WithEndpoint(telemetryEndpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("init otlp trace exporter: %w", err)
	}

	metricExporter, err := otlpmetricgrpc.New(
		ctx,
		otlpmetricgrpc.WithEndpoint(telemetryEndpoint),
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("init otlp metric exporter: %w", err)
	}

	metricReader := sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(metricFlushInterval))
	tracerProvider, meterProvider := installTelemetryProviders(traceExporter, metricReader)
	return func(ctx context.Context) error {
		metricErr := meterProvider.Shutdown(ctx)
		traceErr := tracerProvider.Shutdown(ctx)
		return errors.Join(metricErr, traceErr)
	}, nil
}

func installTelemetryProviders(
	traceExporter sdktrace.SpanExporter,
	metricReader sdkmetric.Reader,
) (*sdktrace.TracerProvider, *sdkmetric.MeterProvider) {
	res := resource.NewSchemaless(
		attribute.String("service.name", traceServiceName),
		attribute.String("service.namespace", traceServiceEnv),
		attribute.String("sigil.devex.language", languageName),
	)

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
		sdktrace.WithBatcher(traceExporter),
		sdktrace.WithResource(res),
	)
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(metricReader),
		sdkmetric.WithResource(res),
	)

	otel.SetTracerProvider(tracerProvider)
	otel.SetMeterProvider(meterProvider)

	return tracerProvider, meterProvider
}

func intFromEnv(key string, defaultValue int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return defaultValue
	}
	return parsed
}

func stringFromEnv(key, defaultValue string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return defaultValue
	}
	return value
}
