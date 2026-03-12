package sigil_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	sigil "github.com/grafana/sigil/sdks/go/sigil"
	sigilv1 "github.com/grafana/sigil/sdks/go/sigil/internal/gen/sigil/v1"
)

func TestConformance_ConversationTitleSemantics(t *testing.T) {
	testCases := []struct {
		name          string
		startTitle    string
		contextTitle  string
		metadataTitle string
		wantTitle     string
	}{
		{
			name:          "explicit wins",
			startTitle:    "Explicit",
			contextTitle:  "Context",
			metadataTitle: "Meta",
			wantTitle:     "Explicit",
		},
		{
			name:         "context fallback",
			contextTitle: "Context",
			wantTitle:    "Context",
		},
		{
			name:          "metadata fallback",
			metadataTitle: "Meta",
			wantTitle:     "Meta",
		},
		{
			name:       "whitespace omitted",
			startTitle: "  ",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			env := newConformanceEnv(t)

			ctx := context.Background()
			if tc.contextTitle != "" {
				ctx = sigil.WithConversationTitle(ctx, tc.contextTitle)
			}

			start := sigil.GenerationStart{
				Model:             conformanceModel,
				ConversationTitle: tc.startTitle,
			}
			if tc.metadataTitle != "" {
				start.Metadata = map[string]any{
					metadataKeyConversation: tc.metadataTitle,
				}
			}

			recordGeneration(t, env, ctx, start, sigil.Generation{})

			span := findSpan(t, env.Spans.Ended(), conformanceOperationName)
			attrs := spanAttrs(span)
			if tc.wantTitle == "" {
				requireSpanAttrAbsent(t, attrs, spanAttrConversationTitle)
			} else {
				requireSpanAttr(t, attrs, spanAttrConversationTitle, tc.wantTitle)
			}

			requireSyncGenerationMetrics(t, env)
			env.Shutdown(t)

			generation := env.Ingest.SingleGeneration(t)
			if tc.wantTitle == "" {
				requireProtoMetadataAbsent(t, generation, metadataKeyConversation)
			} else {
				requireProtoMetadata(t, generation, metadataKeyConversation, tc.wantTitle)
			}
		})
	}
}

func TestConformance_UserIDSemantics(t *testing.T) {
	testCases := []struct {
		name           string
		startUserID    string
		contextUserID  string
		canonicalUser  string
		legacyUser     string
		wantResolvedID string
	}{
		{
			name:           "explicit wins",
			startUserID:    "explicit",
			contextUserID:  "ctx",
			canonicalUser:  "meta-canonical",
			legacyUser:     "meta-legacy",
			wantResolvedID: "explicit",
		},
		{
			name:           "context fallback",
			contextUserID:  "ctx",
			wantResolvedID: "ctx",
		},
		{
			name:           "canonical metadata",
			canonicalUser:  "canonical",
			wantResolvedID: "canonical",
		},
		{
			name:           "legacy metadata",
			legacyUser:     "legacy",
			wantResolvedID: "legacy",
		},
		{
			name:           "canonical beats legacy",
			canonicalUser:  "canonical",
			legacyUser:     "legacy",
			wantResolvedID: "canonical",
		},
		{
			name:           "whitespace trimmed",
			startUserID:    "  padded  ",
			wantResolvedID: "padded",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			env := newConformanceEnv(t)

			ctx := context.Background()
			if tc.contextUserID != "" {
				ctx = sigil.WithUserID(ctx, tc.contextUserID)
			}

			start := sigil.GenerationStart{
				Model:  conformanceModel,
				UserID: tc.startUserID,
			}
			if tc.canonicalUser != "" || tc.legacyUser != "" {
				start.Metadata = map[string]any{}
				if tc.canonicalUser != "" {
					start.Metadata[metadataKeyCanonicalUserID] = tc.canonicalUser
				}
				if tc.legacyUser != "" {
					start.Metadata[metadataKeyLegacyUserID] = tc.legacyUser
				}
			}

			recordGeneration(t, env, ctx, start, sigil.Generation{})

			span := findSpan(t, env.Spans.Ended(), conformanceOperationName)
			attrs := spanAttrs(span)
			requireSpanAttr(t, attrs, spanAttrUserID, tc.wantResolvedID)

			requireSyncGenerationMetrics(t, env)
			env.Shutdown(t)

			generation := env.Ingest.SingleGeneration(t)
			requireProtoMetadata(t, generation, metadataKeyCanonicalUserID, tc.wantResolvedID)
		})
	}
}

func TestConformance_AgentIdentitySemantics(t *testing.T) {
	testCases := []struct {
		name             string
		startAgentName   string
		startVersion     string
		contextAgentName string
		contextVersion   string
		resultAgentName  string
		resultVersion    string
		wantAgentName    string
		wantVersion      string
	}{
		{
			name:           "explicit fields",
			startAgentName: "agent-explicit",
			startVersion:   "v1.2.3",
			wantAgentName:  "agent-explicit",
			wantVersion:    "v1.2.3",
		},
		{
			name:             "context fallback",
			contextAgentName: "agent-context",
			contextVersion:   "v-context",
			wantAgentName:    "agent-context",
			wantVersion:      "v-context",
		},
		{
			name:            "result-time override",
			startAgentName:  "agent-seed",
			startVersion:    "v-seed",
			resultAgentName: "agent-result",
			resultVersion:   "v-result",
			wantAgentName:   "agent-result",
			wantVersion:     "v-result",
		},
		{
			name: "empty field omission",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			env := newConformanceEnv(t)

			ctx := context.Background()
			if tc.contextAgentName != "" {
				ctx = sigil.WithAgentName(ctx, tc.contextAgentName)
			}
			if tc.contextVersion != "" {
				ctx = sigil.WithAgentVersion(ctx, tc.contextVersion)
			}

			start := sigil.GenerationStart{
				Model:        conformanceModel,
				AgentName:    tc.startAgentName,
				AgentVersion: tc.startVersion,
			}
			result := sigil.Generation{
				AgentName:    tc.resultAgentName,
				AgentVersion: tc.resultVersion,
			}

			recordGeneration(t, env, ctx, start, result)

			span := findSpan(t, env.Spans.Ended(), conformanceOperationName)
			attrs := spanAttrs(span)
			if tc.wantAgentName == "" {
				requireSpanAttrAbsent(t, attrs, spanAttrAgentName)
			} else {
				requireSpanAttr(t, attrs, spanAttrAgentName, tc.wantAgentName)
			}
			if tc.wantVersion == "" {
				requireSpanAttrAbsent(t, attrs, spanAttrAgentVersion)
			} else {
				requireSpanAttr(t, attrs, spanAttrAgentVersion, tc.wantVersion)
			}

			requireSyncGenerationMetrics(t, env)
			env.Shutdown(t)

			generation := env.Ingest.SingleGeneration(t)
			if tc.wantAgentName == "" {
				if got := generation.GetAgentName(); got != "" {
					t.Fatalf("expected empty proto agent_name, got %q", got)
				}
			} else if got := generation.GetAgentName(); got != tc.wantAgentName {
				t.Fatalf("unexpected proto agent_name: got %q want %q", got, tc.wantAgentName)
			}

			if tc.wantVersion == "" {
				if got := generation.GetAgentVersion(); got != "" {
					t.Fatalf("expected empty proto agent_version, got %q", got)
				}
			} else if got := generation.GetAgentVersion(); got != tc.wantVersion {
				t.Fatalf("unexpected proto agent_version: got %q want %q", got, tc.wantVersion)
			}
		})
	}
}

func TestConformance_StreamingModeSemantics(t *testing.T) {
	env := newConformanceEnv(t)

	_, recorder := env.Client.StartStreamingGeneration(context.Background(), sigil.GenerationStart{
		ConversationID: "conv-stream",
		Model:          conformanceModel,
	})
	recorder.SetFirstTokenAt(time.Now())
	recorder.SetResult(sigil.Generation{
		Input:  []sigil.Message{sigil.UserTextMessage("Say hello")},
		Output: []sigil.Message{sigil.AssistantTextMessage("Hello world")},
		Usage:  sigil.TokenUsage{InputTokens: 5, OutputTokens: 2},
	}, nil)
	recorder.End()
	if err := recorder.Err(); err != nil {
		t.Fatalf("record streaming generation: %v", err)
	}

	metrics := env.CollectMetrics(t)
	if len(findHistogram[float64](t, metrics, metricOperationDuration).DataPoints) == 0 {
		t.Fatalf("expected %s datapoints for streaming conformance", metricOperationDuration)
	}
	if len(findHistogram[float64](t, metrics, metricTimeToFirstToken).DataPoints) == 0 {
		t.Fatalf("expected %s datapoints for streaming conformance", metricTimeToFirstToken)
	}

	env.Shutdown(t)

	generation := env.Ingest.SingleGeneration(t)
	if generation.GetMode() != sigilv1.GenerationMode_GENERATION_MODE_STREAM {
		t.Fatalf("expected streamed proto mode, got %s", generation.GetMode())
	}
	if generation.GetOperationName() != "streamText" {
		t.Fatalf("expected streamed operation streamText, got %q", generation.GetOperationName())
	}
	if len(generation.GetOutput()) != 1 || len(generation.GetOutput()[0].GetParts()) != 1 {
		t.Fatalf("expected a single streamed assistant output, got %#v", generation.GetOutput())
	}
	if got := generation.GetOutput()[0].GetParts()[0].GetText(); got != "Hello world" {
		t.Fatalf("unexpected streamed assistant text: got %q want %q", got, "Hello world")
	}

	span := findSpan(t, env.Spans.Ended(), "streamText")
	if span.Name() != "streamText gpt-5" {
		t.Fatalf("unexpected streaming span name: %q", span.Name())
	}
}

func TestConformance_ToolExecutionSemantics(t *testing.T) {
	env := newConformanceEnv(t)

	_, recorder := env.Client.StartToolExecution(context.Background(), sigil.ToolExecutionStart{
		ToolName:          "weather",
		ToolCallID:        "call-weather",
		ToolType:          "function",
		ToolDescription:   "Get weather for a city",
		ConversationID:    "conv-tools",
		ConversationTitle: "Weather lookup",
		AgentName:         "assistant-core",
		AgentVersion:      "2026.03.12",
		IncludeContent:    true,
	})
	recorder.SetResult(sigil.ToolExecutionEnd{
		Arguments: map[string]any{"city": "Paris"},
		Result:    map[string]any{"temp_c": 18},
	})
	recorder.End()
	if err := recorder.Err(); err != nil {
		t.Fatalf("record tool execution: %v", err)
	}

	metrics := env.CollectMetrics(t)
	if len(findHistogram[float64](t, metrics, metricOperationDuration).DataPoints) == 0 {
		t.Fatalf("expected %s datapoints for tool execution", metricOperationDuration)
	}
	requireNoHistogram(t, metrics, metricTimeToFirstToken)
	if got := env.Ingest.RequestCount(); got != 0 {
		t.Fatalf("expected no generation exports for tool execution, got %d", got)
	}

	span := findSpan(t, env.Spans.Ended(), "execute_tool")
	attrs := spanAttrs(span)
	requireSpanAttr(t, attrs, spanAttrToolName, "weather")
	requireSpanAttr(t, attrs, spanAttrToolCallID, "call-weather")
	requireSpanAttr(t, attrs, spanAttrToolType, "function")
	requireSpanAttr(t, attrs, spanAttrConversationTitle, "Weather lookup")
	requireSpanAttr(t, attrs, spanAttrAgentName, "assistant-core")
	requireSpanAttr(t, attrs, spanAttrAgentVersion, "2026.03.12")
	requireSpanAttr(t, attrs, spanAttrToolCallArguments, `{"city":"Paris"}`)
	requireSpanAttr(t, attrs, spanAttrToolCallResult, `{"temp_c":18}`)

	env.Shutdown(t)
	if got := env.Ingest.RequestCount(); got != 0 {
		t.Fatalf("expected no generation exports after tool shutdown, got %d", got)
	}
}

func TestConformance_EmbeddingSemantics(t *testing.T) {
	env := newConformanceEnv(t)
	dimensions := int64(256)

	_, recorder := env.Client.StartEmbedding(context.Background(), sigil.EmbeddingStart{
		Model:          sigil.ModelRef{Provider: "openai", Name: "text-embedding-3-small"},
		AgentName:      "agent-embed",
		AgentVersion:   "v-embed",
		Dimensions:     &dimensions,
		EncodingFormat: "float",
	})
	recorder.SetResult(sigil.EmbeddingResult{
		InputCount:    2,
		InputTokens:   120,
		ResponseModel: "text-embedding-3-small",
		Dimensions:    &dimensions,
	})
	recorder.End()
	if err := recorder.Err(); err != nil {
		t.Fatalf("record embedding: %v", err)
	}

	metrics := env.CollectMetrics(t)
	if len(findHistogram[float64](t, metrics, metricOperationDuration).DataPoints) == 0 {
		t.Fatalf("expected %s datapoints for embeddings", metricOperationDuration)
	}
	if len(findHistogram[int64](t, metrics, metricTokenUsage).DataPoints) == 0 {
		t.Fatalf("expected %s datapoints for embeddings", metricTokenUsage)
	}
	requireNoHistogram(t, metrics, metricTimeToFirstToken)
	requireNoHistogram(t, metrics, metricToolCallsPerOperation)
	if got := env.Ingest.RequestCount(); got != 0 {
		t.Fatalf("expected no generation exports for embeddings, got %d", got)
	}

	span := findSpan(t, env.Spans.Ended(), "embeddings")
	attrs := spanAttrs(span)
	requireSpanAttr(t, attrs, spanAttrAgentName, "agent-embed")
	requireSpanAttr(t, attrs, spanAttrAgentVersion, "v-embed")
	if got := attrs[spanAttrEmbeddingInputCount].AsInt64(); got != 2 {
		t.Fatalf("unexpected embedding input count: got %d want %d", got, 2)
	}
	if got := attrs[spanAttrEmbeddingDimCount].AsInt64(); got != dimensions {
		t.Fatalf("unexpected embedding dimension count: got %d want %d", got, dimensions)
	}

	env.Shutdown(t)
	if got := env.Ingest.RequestCount(); got != 0 {
		t.Fatalf("expected no generation exports after embedding shutdown, got %d", got)
	}
}

func TestConformance_ValidationAndErrorSemantics(t *testing.T) {
	t.Run("validation failures stay local and unexported", func(t *testing.T) {
		env := newConformanceEnv(t)

		_, recorder := env.Client.StartGeneration(context.Background(), sigil.GenerationStart{
			ConversationID: "conv-validation",
			Model:          conformanceModel,
		})
		recorder.SetResult(sigil.Generation{
			Input:  []sigil.Message{{Role: sigil.RoleUser}},
			Output: []sigil.Message{sigil.AssistantTextMessage("ok")},
		}, nil)
		recorder.End()

		err := recorder.Err()
		if err == nil {
			t.Fatalf("expected validation error")
		}
		if !errors.Is(err, sigil.ErrValidationFailed) {
			t.Fatalf("expected ErrValidationFailed, got %v", err)
		}

		span := findSpan(t, env.Spans.Ended(), conformanceOperationName)
		attrs := spanAttrs(span)
		requireSpanAttr(t, attrs, spanAttrErrorType, "validation_error")

		env.Shutdown(t)
		if got := env.Ingest.RequestCount(); got != 0 {
			t.Fatalf("expected no generation exports for validation failure, got %d", got)
		}
	})

	t.Run("provider call errors export call error metadata", func(t *testing.T) {
		env := newConformanceEnv(t)

		_, recorder := env.Client.StartGeneration(context.Background(), sigil.GenerationStart{
			ConversationID: "conv-call-error",
			Model:          conformanceModel,
		})
		recorder.SetCallError(errors.New("provider unavailable"))
		recorder.End()
		if err := recorder.Err(); err != nil {
			t.Fatalf("expected nil local error for provider call failure, got %v", err)
		}

		span := findSpan(t, env.Spans.Ended(), conformanceOperationName)
		attrs := spanAttrs(span)
		requireSpanAttr(t, attrs, spanAttrErrorType, "provider_call_error")

		env.Shutdown(t)

		generation := env.Ingest.SingleGeneration(t)
		if got := generation.GetCallError(); got != "provider unavailable" {
			t.Fatalf("unexpected proto call error: got %q want %q", got, "provider unavailable")
		}
		requireProtoMetadata(t, generation, "call_error", "provider unavailable")
	})
}

func TestConformance_RatingSubmissionSemantics(t *testing.T) {
	env := newConformanceEnv(t)

	response, err := env.Client.SubmitConversationRating(context.Background(), "conv-1", sigil.ConversationRatingInput{
		RatingID: "rat-1",
		Rating:   sigil.ConversationRatingValueGood,
		Comment:  "helpful",
		Metadata: map[string]any{"channel": "assistant"},
	})
	if err != nil {
		t.Fatalf("submit rating: %v", err)
	}

	request := env.Rating.SingleRequest(t)
	if request.Method != http.MethodPost {
		t.Fatalf("expected POST rating request, got %s", request.Method)
	}
	if request.Path != "/api/v1/conversations/conv-1/ratings" {
		t.Fatalf("unexpected rating request path: %s", request.Path)
	}

	var body sigil.ConversationRatingInput
	if err := json.Unmarshal(request.Body, &body); err != nil {
		t.Fatalf("decode rating request body: %v", err)
	}
	if body.RatingID != "rat-1" || body.Rating != sigil.ConversationRatingValueGood {
		t.Fatalf("unexpected rating request body: %#v", body)
	}
	if got := body.Metadata["channel"]; got != "assistant" {
		t.Fatalf("expected rating metadata channel=assistant, got %#v", got)
	}
	if response == nil || response.Rating.ConversationID != "conv-1" {
		t.Fatalf("unexpected rating response: %#v", response)
	}
}

func TestConformance_ShutdownFlushSemantics(t *testing.T) {
	env := newConformanceEnv(t, withConformanceConfig(func(cfg *sigil.Config) {
		cfg.GenerationExport.BatchSize = 8
		cfg.GenerationExport.QueueSize = 8
		cfg.GenerationExport.FlushInterval = time.Hour
	}))

	recordGeneration(t, env, context.Background(), sigil.GenerationStart{
		ConversationID: "conv-shutdown",
		Model:          conformanceModel,
	}, sigil.Generation{
		Input:  []sigil.Message{sigil.UserTextMessage("hello")},
		Output: []sigil.Message{sigil.AssistantTextMessage("hi")},
	})

	if got := env.Ingest.RequestCount(); got != 0 {
		t.Fatalf("expected no export before shutdown flush, got %d", got)
	}

	env.Shutdown(t)

	if got := env.Ingest.RequestCount(); got != 1 {
		t.Fatalf("expected one export after shutdown flush, got %d", got)
	}
	generation := env.Ingest.SingleGeneration(t)
	if generation.GetConversationId() != "conv-shutdown" {
		t.Fatalf("unexpected shutdown-flushed conversation id: %q", generation.GetConversationId())
	}
}

func recordGeneration(t *testing.T, env *conformanceEnv, ctx context.Context, start sigil.GenerationStart, result sigil.Generation) {
	t.Helper()

	_, recorder := env.Client.StartGeneration(ctx, start)
	recorder.SetResult(result, nil)
	recorder.End()
	if err := recorder.Err(); err != nil {
		t.Fatalf("record generation: %v", err)
	}
}

func requireSyncGenerationMetrics(t *testing.T, env *conformanceEnv) {
	t.Helper()

	metrics := env.CollectMetrics(t)
	duration := findHistogram[float64](t, metrics, metricOperationDuration)
	if len(duration.DataPoints) == 0 {
		t.Fatalf("expected %s datapoints for conformance generation", metricOperationDuration)
	}
	requireNoHistogram(t, metrics, metricTimeToFirstToken)
}
