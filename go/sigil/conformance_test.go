package sigil_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	sigil "github.com/grafana/sigil/sdks/go/sigil"
	sigilv1 "github.com/grafana/sigil/sdks/go/sigil/internal/gen/sigil/v1"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace"
)

func TestConformance_FullGenerationRoundtrip(t *testing.T) {
	env := newConformanceEnv(t)

	startedAt := time.Date(2026, time.March, 12, 11, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(250 * time.Millisecond)
	maxTokens := int64(1024)
	temperature := 0.7
	topP := 0.9
	toolChoice := "auto"
	thinkingEnabled := true

	start := sigil.GenerationStart{
		ID:                "gen-roundtrip",
		ConversationID:    "conv-roundtrip",
		ConversationTitle: "Roundtrip Test",
		UserID:            "user-42",
		AgentName:         "test-agent",
		AgentVersion:      "1.0.0",
		Model: sigil.ModelRef{
			Provider: "test-provider",
			Name:     "test-model",
		},
		SystemPrompt: "You are a test assistant.",
		Tools: []sigil.ToolDefinition{
			{
				Name:        "lookupWeather",
				Description: "Look up weather",
				Type:        "function",
				InputSchema: []byte(`{"type":"object","properties":{"city":{"type":"string"}}}`),
				Deferred:    true,
			},
		},
		MaxTokens:       &maxTokens,
		Temperature:     &temperature,
		TopP:            &topP,
		ToolChoice:      &toolChoice,
		ThinkingEnabled: &thinkingEnabled,
		Tags: map[string]string{
			"env":   "conformance",
			"suite": "roundtrip",
		},
		Metadata: map[string]any{
			"custom_key":              "custom_value",
			metadataKeyThinkingBudget: int64(2048),
		},
		StartedAt: startedAt,
	}

	result := sigil.Generation{
		ResponseID:    "resp-1",
		ResponseModel: "test-model-v2",
		Input: []sigil.Message{
			{
				Role:  sigil.RoleUser,
				Name:  "user",
				Parts: []sigil.Part{sigil.TextPart("What's the weather in Paris?")},
			},
			{
				Role: sigil.RoleTool,
				Name: "lookupWeather",
				Parts: []sigil.Part{sigil.ToolResultPart(sigil.ToolResult{
					ToolCallID:  "call-1",
					Name:        "lookupWeather",
					Content:     "18C and clear",
					ContentJSON: []byte(`{"temp_c":18,"condition":"clear"}`),
				})},
			},
		},
		Output: []sigil.Message{
			{
				Role: sigil.RoleAssistant,
				Name: "assistant",
				Parts: []sigil.Part{
					sigil.TextPart("It is 18C and clear."),
					sigil.ThinkingPart("Need weather lookup."),
					sigil.ToolCallPart(sigil.ToolCall{
						ID:        "call-1",
						Name:      "lookupWeather",
						InputJSON: []byte(`{"city":"Paris"}`),
					}),
				},
			},
		},
		Usage: sigil.TokenUsage{
			InputTokens:              120,
			OutputTokens:             42,
			TotalTokens:              162,
			CacheReadInputTokens:     30,
			CacheWriteInputTokens:    7,
			CacheCreationInputTokens: 4,
			ReasoningTokens:          9,
		},
		StopReason:  "end_turn",
		CompletedAt: completedAt,
		Artifacts: []sigil.Artifact{
			{
				Kind:        sigil.ArtifactKindRequest,
				Name:        "request.json",
				ContentType: "application/json",
				Payload:     []byte(`{"request":true}`),
				RecordID:    "rec-request",
				URI:         "sigil://artifact/request",
			},
			{
				Kind:        sigil.ArtifactKindResponse,
				Name:        "response.json",
				ContentType: "application/json",
				Payload:     []byte(`{"response":true}`),
				RecordID:    "rec-response",
				URI:         "sigil://artifact/response",
			},
		},
	}

	_, recorder := env.Client.StartGeneration(context.Background(), start)
	recorder.SetResult(result, nil)
	recorder.End()
	if err := recorder.Err(); err != nil {
		t.Fatalf("record generation: %v", err)
	}

	span := findSpan(t, env.Spans.Ended(), conformanceOperationName)
	if got := span.Name(); got != "generateText test-model" {
		t.Fatalf("unexpected span name: got %q want %q", got, "generateText test-model")
	}
	if got := span.SpanKind(); got != trace.SpanKindClient {
		t.Fatalf("unexpected span kind: got %v want %v", got, trace.SpanKindClient)
	}
	if got := span.Status().Code; got != codes.Ok {
		t.Fatalf("unexpected span status: got %v want %v", got, codes.Ok)
	}

	attrs := spanAttrs(span)
	requireSpanAttr(t, attrs, spanAttrOperationName, conformanceOperationName)
	requireSpanAttr(t, attrs, spanAttrGenerationID, start.ID)
	requireSpanAttr(t, attrs, "gen_ai.conversation.id", start.ConversationID)
	requireSpanAttr(t, attrs, spanAttrConversationTitle, start.ConversationTitle)
	requireSpanAttr(t, attrs, spanAttrUserID, start.UserID)
	requireSpanAttr(t, attrs, spanAttrAgentName, start.AgentName)
	requireSpanAttr(t, attrs, spanAttrAgentVersion, start.AgentVersion)
	requireSpanAttr(t, attrs, spanAttrProviderName, start.Model.Provider)
	requireSpanAttr(t, attrs, spanAttrRequestModel, start.Model.Name)
	requireSpanAttrInt64(t, attrs, spanAttrRequestMaxTokens, maxTokens)
	requireSpanAttrFloat64(t, attrs, spanAttrRequestTemperature, temperature)
	requireSpanAttrFloat64(t, attrs, spanAttrRequestTopP, topP)
	requireSpanAttr(t, attrs, spanAttrRequestToolChoice, toolChoice)
	requireSpanAttrBool(t, attrs, "sigil.gen_ai.request.thinking.enabled", thinkingEnabled)
	requireSpanAttrInt64(t, attrs, "sigil.gen_ai.request.thinking.budget_tokens", 2048)
	requireSpanAttr(t, attrs, spanAttrResponseID, result.ResponseID)
	requireSpanAttr(t, attrs, spanAttrResponseModel, result.ResponseModel)
	requireSpanAttrStringSlice(t, attrs, spanAttrFinishReasons, []string{result.StopReason})
	requireSpanAttrInt64(t, attrs, spanAttrInputTokens, result.Usage.InputTokens)
	requireSpanAttrInt64(t, attrs, spanAttrOutputTokens, result.Usage.OutputTokens)
	requireSpanAttrInt64(t, attrs, spanAttrCacheReadTokens, result.Usage.CacheReadInputTokens)
	requireSpanAttrInt64(t, attrs, spanAttrCacheWriteTokens, result.Usage.CacheWriteInputTokens)
	requireSpanAttrInt64(t, attrs, spanAttrCacheCreationTokens, result.Usage.CacheCreationInputTokens)
	requireSpanAttrInt64(t, attrs, spanAttrReasoningTokens, result.Usage.ReasoningTokens)
	requireSpanAttr(t, attrs, metadataKeySDKName, sdkNameGo)

	env.Shutdown(t)

	generation := env.Ingest.SingleGeneration(t)
	if got := generation.GetId(); got != start.ID {
		t.Fatalf("unexpected proto id: got %q want %q", got, start.ID)
	}
	if got := generation.GetConversationId(); got != start.ConversationID {
		t.Fatalf("unexpected proto conversation_id: got %q want %q", got, start.ConversationID)
	}
	if got := generation.GetOperationName(); got != conformanceOperationName {
		t.Fatalf("unexpected proto operation_name: got %q want %q", got, conformanceOperationName)
	}
	if got := generation.GetMode(); got != sigilv1.GenerationMode_GENERATION_MODE_SYNC {
		t.Fatalf("unexpected proto mode: got %v want %v", got, sigilv1.GenerationMode_GENERATION_MODE_SYNC)
	}
	if got := generation.GetAgentName(); got != start.AgentName {
		t.Fatalf("unexpected proto agent_name: got %q want %q", got, start.AgentName)
	}
	if got := generation.GetAgentVersion(); got != start.AgentVersion {
		t.Fatalf("unexpected proto agent_version: got %q want %q", got, start.AgentVersion)
	}
	if got := generation.GetResponseId(); got != result.ResponseID {
		t.Fatalf("unexpected proto response_id: got %q want %q", got, result.ResponseID)
	}
	if got := generation.GetResponseModel(); got != result.ResponseModel {
		t.Fatalf("unexpected proto response_model: got %q want %q", got, result.ResponseModel)
	}
	if got := generation.GetSystemPrompt(); got != start.SystemPrompt {
		t.Fatalf("unexpected proto system_prompt: got %q want %q", got, start.SystemPrompt)
	}
	if got := generation.GetTraceId(); got != span.SpanContext().TraceID().String() {
		t.Fatalf("unexpected proto trace_id: got %q want %q", got, span.SpanContext().TraceID().String())
	}
	if got := generation.GetSpanId(); got != span.SpanContext().SpanID().String() {
		t.Fatalf("unexpected proto span_id: got %q want %q", got, span.SpanContext().SpanID().String())
	}
	if !generation.GetStartedAt().AsTime().Equal(startedAt) {
		t.Fatalf("unexpected proto started_at: got %s want %s", generation.GetStartedAt().AsTime(), startedAt)
	}
	if !generation.GetCompletedAt().AsTime().Equal(completedAt) {
		t.Fatalf("unexpected proto completed_at: got %s want %s", generation.GetCompletedAt().AsTime(), completedAt)
	}

	if got := generation.GetModel().GetProvider(); got != start.Model.Provider {
		t.Fatalf("unexpected proto model.provider: got %q want %q", got, start.Model.Provider)
	}
	if got := generation.GetModel().GetName(); got != start.Model.Name {
		t.Fatalf("unexpected proto model.name: got %q want %q", got, start.Model.Name)
	}
	if got := generation.GetMaxTokens(); got != maxTokens {
		t.Fatalf("unexpected proto max_tokens: got %d want %d", got, maxTokens)
	}
	if got := generation.GetTemperature(); got != temperature {
		t.Fatalf("unexpected proto temperature: got %v want %v", got, temperature)
	}
	if got := generation.GetTopP(); got != topP {
		t.Fatalf("unexpected proto top_p: got %v want %v", got, topP)
	}
	if got := generation.GetToolChoice(); got != toolChoice {
		t.Fatalf("unexpected proto tool_choice: got %q want %q", got, toolChoice)
	}
	if got := generation.GetThinkingEnabled(); got != thinkingEnabled {
		t.Fatalf("unexpected proto thinking_enabled: got %t want %t", got, thinkingEnabled)
	}

	if len(generation.GetTags()) != len(start.Tags) {
		t.Fatalf("unexpected proto tags length: got %d want %d", len(generation.GetTags()), len(start.Tags))
	}
	for key, want := range start.Tags {
		if got := generation.GetTags()[key]; got != want {
			t.Fatalf("unexpected proto tag %q: got %q want %q", key, got, want)
		}
	}

	requireProtoMetadata(t, generation, "custom_key", "custom_value")
	requireProtoMetadata(t, generation, metadataKeyConversation, start.ConversationTitle)
	requireProtoMetadata(t, generation, metadataKeyCanonicalUserID, start.UserID)
	requireProtoMetadata(t, generation, metadataKeySDKName, sdkNameGo)
	requireProtoMetadataNumber(t, generation, metadataKeyThinkingBudget, 2048)

	tools := generation.GetTools()
	if len(tools) != 1 {
		t.Fatalf("unexpected proto tools length: got %d want %d", len(tools), 1)
	}
	if got := tools[0].GetName(); got != start.Tools[0].Name {
		t.Fatalf("unexpected proto tool name: got %q want %q", got, start.Tools[0].Name)
	}
	if got := tools[0].GetDescription(); got != start.Tools[0].Description {
		t.Fatalf("unexpected proto tool description: got %q want %q", got, start.Tools[0].Description)
	}
	if got := tools[0].GetType(); got != start.Tools[0].Type {
		t.Fatalf("unexpected proto tool type: got %q want %q", got, start.Tools[0].Type)
	}
	if !bytes.Equal(tools[0].GetInputSchemaJson(), start.Tools[0].InputSchema) {
		t.Fatalf("unexpected proto tool input schema: got %s want %s", string(tools[0].GetInputSchemaJson()), string(start.Tools[0].InputSchema))
	}
	if got := tools[0].GetDeferred(); !got {
		t.Fatalf("expected proto tool deferred=true")
	}

	input := generation.GetInput()
	if len(input) != 2 {
		t.Fatalf("unexpected proto input length: got %d want %d", len(input), 2)
	}
	if got := input[0].GetRole(); got != sigilv1.MessageRole_MESSAGE_ROLE_USER {
		t.Fatalf("unexpected first input role: got %v want %v", got, sigilv1.MessageRole_MESSAGE_ROLE_USER)
	}
	requireProtoTextPart(t, input[0].GetParts()[0], "What's the weather in Paris?")
	if got := input[1].GetRole(); got != sigilv1.MessageRole_MESSAGE_ROLE_TOOL {
		t.Fatalf("unexpected second input role: got %v want %v", got, sigilv1.MessageRole_MESSAGE_ROLE_TOOL)
	}
	requireProtoToolResultPart(t, input[1].GetParts()[0], "call-1", "lookupWeather", "18C and clear", []byte(`{"temp_c":18,"condition":"clear"}`), false)

	output := generation.GetOutput()
	if len(output) != 1 {
		t.Fatalf("unexpected proto output length: got %d want %d", len(output), 1)
	}
	if got := output[0].GetRole(); got != sigilv1.MessageRole_MESSAGE_ROLE_ASSISTANT {
		t.Fatalf("unexpected output role: got %v want %v", got, sigilv1.MessageRole_MESSAGE_ROLE_ASSISTANT)
	}
	if len(output[0].GetParts()) != 3 {
		t.Fatalf("unexpected output part count: got %d want %d", len(output[0].GetParts()), 3)
	}
	requireProtoTextPart(t, output[0].GetParts()[0], "It is 18C and clear.")
	requireProtoThinkingPart(t, output[0].GetParts()[1], "Need weather lookup.")
	requireProtoToolCallPart(t, output[0].GetParts()[2], "call-1", "lookupWeather", []byte(`{"city":"Paris"}`))

	usage := generation.GetUsage()
	if got := usage.GetInputTokens(); got != result.Usage.InputTokens {
		t.Fatalf("unexpected proto input_tokens: got %d want %d", got, result.Usage.InputTokens)
	}
	if got := usage.GetOutputTokens(); got != result.Usage.OutputTokens {
		t.Fatalf("unexpected proto output_tokens: got %d want %d", got, result.Usage.OutputTokens)
	}
	if got := usage.GetTotalTokens(); got != result.Usage.TotalTokens {
		t.Fatalf("unexpected proto total_tokens: got %d want %d", got, result.Usage.TotalTokens)
	}
	if got := usage.GetCacheReadInputTokens(); got != result.Usage.CacheReadInputTokens {
		t.Fatalf("unexpected proto cache_read_input_tokens: got %d want %d", got, result.Usage.CacheReadInputTokens)
	}
	if got := usage.GetCacheWriteInputTokens(); got != result.Usage.CacheWriteInputTokens {
		t.Fatalf("unexpected proto cache_write_input_tokens: got %d want %d", got, result.Usage.CacheWriteInputTokens)
	}
	if got := usage.GetCacheCreationInputTokens(); got != result.Usage.CacheCreationInputTokens {
		t.Fatalf("unexpected proto cache_creation_input_tokens: got %d want %d", got, result.Usage.CacheCreationInputTokens)
	}
	if got := usage.GetReasoningTokens(); got != result.Usage.ReasoningTokens {
		t.Fatalf("unexpected proto reasoning_tokens: got %d want %d", got, result.Usage.ReasoningTokens)
	}
	if got := generation.GetStopReason(); got != result.StopReason {
		t.Fatalf("unexpected proto stop_reason: got %q want %q", got, result.StopReason)
	}

	artifacts := generation.GetRawArtifacts()
	if len(artifacts) != 2 {
		t.Fatalf("unexpected proto artifacts length: got %d want %d", len(artifacts), 2)
	}
	requireProtoArtifact(t, artifacts[0], sigilv1.ArtifactKind_ARTIFACT_KIND_REQUEST, "request.json", "application/json", []byte(`{"request":true}`), "rec-request", "sigil://artifact/request")
	requireProtoArtifact(t, artifacts[1], sigilv1.ArtifactKind_ARTIFACT_KIND_RESPONSE, "response.json", "application/json", []byte(`{"response":true}`), "rec-response", "sigil://artifact/response")

	metrics := env.CollectMetrics(t)
	duration := findHistogram[float64](t, metrics, metricOperationDuration)
	requireHistogramPointWithAttrs(t, duration, map[string]string{
		spanAttrOperationName: conformanceOperationName,
		spanAttrProviderName:  start.Model.Provider,
		spanAttrRequestModel:  start.Model.Name,
		spanAttrAgentName:     start.AgentName,
	})

	tokenUsage := findHistogram[int64](t, metrics, metricTokenUsage)
	requireInt64HistogramSum(t, tokenUsage, map[string]string{
		spanAttrOperationName: conformanceOperationName,
		spanAttrProviderName:  start.Model.Provider,
		spanAttrRequestModel:  start.Model.Name,
		spanAttrAgentName:     start.AgentName,
		metricAttrTokenType:   metricTokenTypeInput,
	}, result.Usage.InputTokens)
	requireInt64HistogramSum(t, tokenUsage, map[string]string{
		spanAttrOperationName: conformanceOperationName,
		spanAttrProviderName:  start.Model.Provider,
		spanAttrRequestModel:  start.Model.Name,
		spanAttrAgentName:     start.AgentName,
		metricAttrTokenType:   metricTokenTypeOutput,
	}, result.Usage.OutputTokens)
	requireInt64HistogramSum(t, tokenUsage, map[string]string{
		spanAttrOperationName: conformanceOperationName,
		spanAttrProviderName:  start.Model.Provider,
		spanAttrRequestModel:  start.Model.Name,
		spanAttrAgentName:     start.AgentName,
		metricAttrTokenType:   metricTokenTypeCacheRead,
	}, result.Usage.CacheReadInputTokens)
	requireInt64HistogramSum(t, tokenUsage, map[string]string{
		spanAttrOperationName: conformanceOperationName,
		spanAttrProviderName:  start.Model.Provider,
		spanAttrRequestModel:  start.Model.Name,
		spanAttrAgentName:     start.AgentName,
		metricAttrTokenType:   metricTokenTypeCacheWrite,
	}, result.Usage.CacheWriteInputTokens)
	requireInt64HistogramSum(t, tokenUsage, map[string]string{
		spanAttrOperationName: conformanceOperationName,
		spanAttrProviderName:  start.Model.Provider,
		spanAttrRequestModel:  start.Model.Name,
		spanAttrAgentName:     start.AgentName,
		metricAttrTokenType:   metricTokenTypeCacheCreation,
	}, result.Usage.CacheCreationInputTokens)
	requireInt64HistogramSum(t, tokenUsage, map[string]string{
		spanAttrOperationName: conformanceOperationName,
		spanAttrProviderName:  start.Model.Provider,
		spanAttrRequestModel:  start.Model.Name,
		spanAttrAgentName:     start.AgentName,
		metricAttrTokenType:   metricTokenTypeReasoning,
	}, result.Usage.ReasoningTokens)

	toolCalls := findHistogram[int64](t, metrics, metricToolCallsPerOperation)
	requireInt64HistogramSum(t, toolCalls, map[string]string{
		spanAttrProviderName: start.Model.Provider,
		spanAttrRequestModel: start.Model.Name,
		spanAttrAgentName:    start.AgentName,
	}, 1)
	requireNoHistogram(t, metrics, metricTimeToFirstToken)
}

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

func TestConformance_StreamingMode(t *testing.T) {
	env := newConformanceEnv(t)

	recordGeneration(t, env, context.Background(), sigil.GenerationStart{
		ConversationID: "conv-sync",
		Model:          conformanceModel,
		StartedAt:      time.Date(2026, 3, 12, 14, 0, 0, 0, time.UTC),
	}, sigil.Generation{
		Input:       []sigil.Message{sigil.UserTextMessage("hello")},
		Output:      []sigil.Message{sigil.AssistantTextMessage("hi")},
		CompletedAt: time.Date(2026, 3, 12, 14, 0, 1, 0, time.UTC),
	})

	streamStartedAt := time.Date(2026, 3, 12, 14, 1, 0, 0, time.UTC)
	_, recorder := env.Client.StartStreamingGeneration(context.Background(), sigil.GenerationStart{
		ConversationID: "conv-stream",
		AgentName:      "agent-stream",
		Model:          conformanceModel,
		StartedAt:      streamStartedAt,
	})
	recorder.SetFirstTokenAt(streamStartedAt.Add(250 * time.Millisecond))
	recorder.SetResult(sigil.Generation{
		Input:       []sigil.Message{sigil.UserTextMessage("say hello")},
		Output:      []sigil.Message{sigil.AssistantTextMessage("Hello world")},
		CompletedAt: streamStartedAt.Add(1500 * time.Millisecond),
	}, nil)
	recorder.End()
	if err := recorder.Err(); err != nil {
		t.Fatalf("record streaming generation: %v", err)
	}

	metrics := env.CollectMetrics(t)
	ttft := findHistogram[float64](t, metrics, metricTimeToFirstToken)
	if len(ttft.DataPoints) != 1 {
		t.Fatalf("expected exactly 1 %s datapoint, got %d", metricTimeToFirstToken, len(ttft.DataPoints))
	}
	requireHistogramPointWithAttrs(t, ttft, map[string]string{
		spanAttrProviderName: conformanceModel.Provider,
		spanAttrRequestModel: conformanceModel.Name,
		spanAttrAgentName:    "agent-stream",
	})

	env.Shutdown(t)

	streamGeneration := findGenerationByConversationID(t, env.Ingest.Requests(), "conv-stream")
	if got := streamGeneration.GetMode(); got != sigilv1.GenerationMode_GENERATION_MODE_STREAM {
		t.Fatalf("unexpected proto mode: got %v want %v", got, sigilv1.GenerationMode_GENERATION_MODE_STREAM)
	}
	if got := streamGeneration.GetOperationName(); got != conformanceStreamOperation {
		t.Fatalf("unexpected proto operation: got %q want %q", got, conformanceStreamOperation)
	}
	if len(streamGeneration.GetOutput()) != 1 || len(streamGeneration.GetOutput()[0].GetParts()) != 1 {
		t.Fatalf("expected a single streamed assistant output, got %#v", streamGeneration.GetOutput())
	}
	if got := streamGeneration.GetOutput()[0].GetParts()[0].GetText(); got != "Hello world" {
		t.Fatalf("unexpected streamed assistant text: got %q want %q", got, "Hello world")
	}

	span := findSpan(t, env.Spans.Ended(), conformanceStreamOperation)
	if got := span.Name(); got != conformanceStreamOperation+" "+conformanceModel.Name {
		t.Fatalf("unexpected streaming span name: %q", got)
	}
	attrs := spanAttrs(span)
	requireSpanAttr(t, attrs, spanAttrOperationName, conformanceStreamOperation)
}

func TestConformance_ToolExecution(t *testing.T) {
	env := newConformanceEnv(t)

	ctx := sigil.WithConversationID(context.Background(), "conv-tool")
	ctx = sigil.WithConversationTitle(ctx, "Weather lookup")
	ctx = sigil.WithAgentName(ctx, "agent-tools")
	ctx = sigil.WithAgentVersion(ctx, "2026.03.12")

	generationStartedAt := time.Date(2026, 3, 12, 14, 2, 0, 0, time.UTC)
	callCtx, generationRecorder := env.Client.StartGeneration(ctx, sigil.GenerationStart{
		Model:     conformanceModel,
		StartedAt: generationStartedAt,
	})
	_, toolRecorder := env.Client.StartToolExecution(callCtx, sigil.ToolExecutionStart{
		ToolName:        "weather",
		ToolCallID:      "call-weather",
		ToolType:        "function",
		ToolDescription: "Get weather",
		IncludeContent:  true,
		StartedAt:       generationStartedAt.Add(100 * time.Millisecond),
	})
	toolRecorder.SetResult(sigil.ToolExecutionEnd{
		Arguments:   map[string]any{"city": "Paris"},
		Result:      map[string]any{"temp_c": 18},
		CompletedAt: generationStartedAt.Add(600 * time.Millisecond),
	})
	toolRecorder.End()
	if err := toolRecorder.Err(); err != nil {
		t.Fatalf("record tool execution: %v", err)
	}

	generationRecorder.SetResult(sigil.Generation{
		Input:       []sigil.Message{sigil.UserTextMessage("weather in Paris")},
		Output:      []sigil.Message{sigil.AssistantTextMessage("Paris is 18C")},
		CompletedAt: generationStartedAt.Add(time.Second),
	}, nil)
	generationRecorder.End()
	if err := generationRecorder.Err(); err != nil {
		t.Fatalf("record parent generation: %v", err)
	}

	metrics := env.CollectMetrics(t)
	duration := findHistogram[float64](t, metrics, metricOperationDuration)
	requireHistogramPointWithAttrs(t, duration, map[string]string{
		spanAttrOperationName: conformanceToolOperation,
		spanAttrRequestModel:  "weather",
		spanAttrAgentName:     "agent-tools",
	})

	env.Shutdown(t)

	span := findSpan(t, env.Spans.Ended(), conformanceToolOperation)
	if got := span.SpanKind(); got != trace.SpanKindInternal {
		t.Fatalf("unexpected tool span kind: got %v want %v", got, trace.SpanKindInternal)
	}

	attrs := spanAttrs(span)
	requireSpanAttr(t, attrs, spanAttrOperationName, conformanceToolOperation)
	requireSpanAttr(t, attrs, spanAttrToolName, "weather")
	requireSpanAttr(t, attrs, spanAttrToolCallID, "call-weather")
	requireSpanAttr(t, attrs, spanAttrToolType, "function")
	requireSpanAttr(t, attrs, spanAttrToolDescription, "Get weather")
	requireSpanAttr(t, attrs, spanAttrConversationID, "conv-tool")
	requireSpanAttr(t, attrs, spanAttrConversationTitle, "Weather lookup")
	requireSpanAttr(t, attrs, spanAttrAgentName, "agent-tools")
	requireSpanAttr(t, attrs, spanAttrAgentVersion, "2026.03.12")
	requireSpanAttr(t, attrs, metadataKeySDKName, sdkNameGo)
	requireSpanAttrPresent(t, attrs, spanAttrToolCallArguments)
	requireSpanAttrPresent(t, attrs, spanAttrToolCallResult)
}

func TestConformance_Embedding(t *testing.T) {
	env := newConformanceEnv(t)

	_, recorder := env.Client.StartEmbedding(context.Background(), sigil.EmbeddingStart{
		Model:          sigil.ModelRef{Provider: "openai", Name: "text-embedding-3-small"},
		AgentName:      "agent-embed",
		Dimensions:     int64Ptr(256),
		EncodingFormat: "float",
		StartedAt:      time.Date(2026, 3, 12, 14, 3, 0, 0, time.UTC),
	})
	recorder.SetResult(sigil.EmbeddingResult{
		InputCount:    2,
		InputTokens:   120,
		ResponseModel: "text-embedding-3-small",
		Dimensions:    int64Ptr(256),
	})
	recorder.End()
	if err := recorder.Err(); err != nil {
		t.Fatalf("record embedding: %v", err)
	}

	metrics := env.CollectMetrics(t)
	duration := findHistogram[float64](t, metrics, metricOperationDuration)
	requireHistogramPointWithAttrs(t, duration, map[string]string{
		spanAttrOperationName: conformanceEmbeddingOperation,
		spanAttrProviderName:  "openai",
		spanAttrRequestModel:  "text-embedding-3-small",
		spanAttrAgentName:     "agent-embed",
	})
	tokenUsage := findHistogram[int64](t, metrics, metricTokenUsage)
	requireHistogramPointWithAttrs(t, tokenUsage, map[string]string{
		spanAttrOperationName: conformanceEmbeddingOperation,
		spanAttrProviderName:  "openai",
		spanAttrRequestModel:  "text-embedding-3-small",
		spanAttrAgentName:     "agent-embed",
		metricAttrTokenType:   metricTokenTypeInput,
	})
	requireNoHistogram(t, metrics, metricTimeToFirstToken)
	requireNoHistogram(t, metrics, metricToolCallsPerOperation)

	env.Shutdown(t)

	if got := env.Ingest.GenerationCount(); got != 0 {
		t.Fatalf("expected no generation exports for embeddings, got %d", got)
	}

	span := findSpan(t, env.Spans.Ended(), conformanceEmbeddingOperation)
	if got := span.SpanKind(); got != trace.SpanKindClient {
		t.Fatalf("unexpected embedding span kind: got %v want %v", got, trace.SpanKindClient)
	}

	attrs := spanAttrs(span)
	requireSpanAttr(t, attrs, spanAttrOperationName, conformanceEmbeddingOperation)
	requireSpanAttr(t, attrs, spanAttrProviderName, "openai")
	requireSpanAttr(t, attrs, spanAttrRequestModel, "text-embedding-3-small")
	requireSpanAttr(t, attrs, metadataKeySDKName, sdkNameGo)
	if got := attrs[spanAttrEmbeddingInputCount].AsInt64(); got != 2 {
		t.Fatalf("unexpected embedding input count: got %d want 2", got)
	}
	if got := attrs[spanAttrEmbeddingDimCount].AsInt64(); got != 256 {
		t.Fatalf("unexpected embedding dimension count: got %d want 256", got)
	}
}

func TestConformance_ValidationAndErrorSemantics(t *testing.T) {
	t.Run("invalid generation", func(t *testing.T) {
		env := newConformanceEnv(t)

		_, recorder := env.Client.StartGeneration(context.Background(), sigil.GenerationStart{
			ConversationID: "conv-invalid",
			StartedAt:      time.Date(2026, 3, 12, 14, 4, 0, 0, time.UTC),
		})
		recorder.SetResult(sigil.Generation{
			Input:       []sigil.Message{sigil.UserTextMessage("hello")},
			Output:      []sigil.Message{sigil.AssistantTextMessage("hi")},
			CompletedAt: time.Date(2026, 3, 12, 14, 4, 1, 0, time.UTC),
		}, nil)
		recorder.End()

		if err := recorder.Err(); !errors.Is(err, sigil.ErrValidationFailed) {
			t.Fatalf("expected ErrValidationFailed, got %v", err)
		}
		if got := env.Ingest.GenerationCount(); got != 0 {
			t.Fatalf("expected no exports for invalid generation, got %d", got)
		}

		span := findSpan(t, env.Spans.Ended(), conformanceOperationName)
		if got := span.Status().Code; got != codes.Error {
			t.Fatalf("expected error span status, got %v", got)
		}
		attrs := spanAttrs(span)
		requireSpanAttr(t, attrs, spanAttrErrorType, "validation_error")
	})

	t.Run("provider call error", func(t *testing.T) {
		env := newConformanceEnv(t)

		_, recorder := env.Client.StartGeneration(context.Background(), sigil.GenerationStart{
			ConversationID: "conv-rate-limit",
			AgentName:      "agent-error",
			Model:          conformanceModel,
			StartedAt:      time.Date(2026, 3, 12, 14, 5, 0, 0, time.UTC),
		})
		recorder.SetCallError(errors.New("provider returned HTTP 429 rate limit"))
		recorder.SetResult(sigil.Generation{
			Input:       []sigil.Message{sigil.UserTextMessage("retry later")},
			Output:      []sigil.Message{sigil.AssistantTextMessage("rate limited")},
			CompletedAt: time.Date(2026, 3, 12, 14, 5, 1, 0, time.UTC),
		}, nil)
		recorder.End()
		if err := recorder.Err(); err != nil {
			t.Fatalf("expected no local error for provider call failure, got %v", err)
		}

		metrics := env.CollectMetrics(t)
		duration := findHistogram[float64](t, metrics, metricOperationDuration)
		requireHistogramPointWithAttrs(t, duration, map[string]string{
			spanAttrOperationName: conformanceOperationName,
			spanAttrProviderName:  conformanceModel.Provider,
			spanAttrRequestModel:  conformanceModel.Name,
			spanAttrAgentName:     "agent-error",
			spanAttrErrorType:     "provider_call_error",
			spanAttrErrorCategory: "rate_limit",
		})

		env.Shutdown(t)

		span := findSpan(t, env.Spans.Ended(), conformanceOperationName)
		if got := span.Status().Code; got != codes.Error {
			t.Fatalf("expected error span status, got %v", got)
		}
		attrs := spanAttrs(span)
		requireSpanAttr(t, attrs, spanAttrErrorType, "provider_call_error")
		requireSpanAttr(t, attrs, spanAttrErrorCategory, "rate_limit")

		generation := env.Ingest.SingleGeneration(t)
		if got := generation.GetCallError(); got != "provider returned HTTP 429 rate limit" {
			t.Fatalf("unexpected proto call error: got %q", got)
		}
		requireProtoMetadata(t, generation, "call_error", "provider returned HTTP 429 rate limit")
	})
}

func TestConformance_RatingHelper(t *testing.T) {
	env := newConformanceEnv(t, withConformanceConfig(func(cfg *sigil.Config) {
		cfg.GenerationExport.Headers = map[string]string{"X-Custom": "test"}
	}))

	response, err := env.Client.SubmitConversationRating(context.Background(), "conv-rated", sigil.ConversationRatingInput{
		RatingID: "rat-1",
		Rating:   sigil.ConversationRatingValueGood,
		Comment:  "looks good",
		Metadata: map[string]any{"channel": "assistant"},
	})
	if err != nil {
		t.Fatalf("submit conversation rating: %v", err)
	}

	requests := env.Rating.Requests()
	if len(requests) != 1 {
		t.Fatalf("expected exactly 1 rating request, got %d", len(requests))
	}

	request := requests[0]
	if request.Method != http.MethodPost {
		t.Fatalf("unexpected request method: got %s want %s", request.Method, http.MethodPost)
	}
	if request.Path != "/api/v1/conversations/conv-rated/ratings" {
		t.Fatalf("unexpected rating request path: %s", request.Path)
	}
	if got := request.Headers.Get("X-Custom"); got != "test" {
		t.Fatalf("expected X-Custom header, got %q", got)
	}

	var payload sigil.ConversationRatingInput
	if err := json.Unmarshal(request.Body, &payload); err != nil {
		t.Fatalf("decode rating request body: %v", err)
	}
	if payload.RatingID != "rat-1" {
		t.Fatalf("unexpected rating id: %q", payload.RatingID)
	}
	if payload.Rating != sigil.ConversationRatingValueGood {
		t.Fatalf("unexpected rating value: %q", payload.Rating)
	}
	if payload.Comment != "looks good" {
		t.Fatalf("unexpected comment: %q", payload.Comment)
	}
	if got := payload.Metadata["channel"]; got != "assistant" {
		t.Fatalf("unexpected metadata: %#v", payload.Metadata)
	}
	if response == nil || response.Rating.RatingID != "rat-1" {
		t.Fatalf("unexpected rating response: %#v", response)
	}
}

func TestConformance_ShutdownFlushesPendingGeneration(t *testing.T) {
	env := newConformanceEnv(t, withConformanceConfig(func(cfg *sigil.Config) {
		cfg.GenerationExport.BatchSize = 10
	}))

	recordGeneration(t, env, context.Background(), sigil.GenerationStart{
		ConversationID: "conv-shutdown",
		Model:          conformanceModel,
		StartedAt:      time.Date(2026, 3, 12, 14, 6, 0, 0, time.UTC),
	}, sigil.Generation{
		Input:       []sigil.Message{sigil.UserTextMessage("hello")},
		Output:      []sigil.Message{sigil.AssistantTextMessage("hi")},
		CompletedAt: time.Date(2026, 3, 12, 14, 6, 1, 0, time.UTC),
	})

	if got := env.Ingest.GenerationCount(); got != 0 {
		t.Fatalf("expected no exports before shutdown flush, got %d", got)
	}

	env.Shutdown(t)

	if got := env.Ingest.GenerationCount(); got != 1 {
		t.Fatalf("expected exactly 1 exported generation after shutdown, got %d", got)
	}
	generation := env.Ingest.SingleGeneration(t)
	if got := generation.GetConversationId(); got != "conv-shutdown" {
		t.Fatalf("unexpected shutdown-flushed conversation id: %q", got)
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

func findGenerationByConversationID(t *testing.T, requests []*sigilv1.ExportGenerationsRequest, conversationID string) *sigilv1.Generation {
	t.Helper()

	for _, req := range requests {
		for _, generation := range req.GetGenerations() {
			if generation.GetConversationId() == conversationID {
				return generation
			}
		}
	}

	t.Fatalf("expected generation for conversation %q", conversationID)
	return nil
}

func int64Ptr(value int64) *int64 {
	return &value
}

func requireProtoMetadataNumber(t *testing.T, generation *sigilv1.Generation, key string, want float64) {
	t.Helper()

	value, ok := generation.GetMetadata().AsMap()[key]
	if !ok {
		t.Fatalf("expected generation metadata %q=%v, key missing", key, want)
	}
	got, ok := value.(float64)
	if !ok {
		t.Fatalf("expected generation metadata %q to be float64, got %#v", key, value)
	}
	if got != want {
		t.Fatalf("unexpected generation metadata %q: got %v want %v", key, got, want)
	}
}

func requireProtoTextPart(t *testing.T, part *sigilv1.Part, want string) {
	t.Helper()

	payload, ok := part.GetPayload().(*sigilv1.Part_Text)
	if !ok {
		t.Fatalf("expected text part, got %T", part.GetPayload())
	}
	if payload.Text != want {
		t.Fatalf("unexpected text part: got %q want %q", payload.Text, want)
	}
}

func requireProtoThinkingPart(t *testing.T, part *sigilv1.Part, want string) {
	t.Helper()

	payload, ok := part.GetPayload().(*sigilv1.Part_Thinking)
	if !ok {
		t.Fatalf("expected thinking part, got %T", part.GetPayload())
	}
	if payload.Thinking != want {
		t.Fatalf("unexpected thinking part: got %q want %q", payload.Thinking, want)
	}
}

func requireProtoToolCallPart(t *testing.T, part *sigilv1.Part, wantID string, wantName string, wantInputJSON []byte) {
	t.Helper()

	payload, ok := part.GetPayload().(*sigilv1.Part_ToolCall)
	if !ok {
		t.Fatalf("expected tool call part, got %T", part.GetPayload())
	}
	if got := payload.ToolCall.GetId(); got != wantID {
		t.Fatalf("unexpected tool call id: got %q want %q", got, wantID)
	}
	if got := payload.ToolCall.GetName(); got != wantName {
		t.Fatalf("unexpected tool call name: got %q want %q", got, wantName)
	}
	if !bytes.Equal(payload.ToolCall.GetInputJson(), wantInputJSON) {
		t.Fatalf("unexpected tool call input_json: got %s want %s", string(payload.ToolCall.GetInputJson()), string(wantInputJSON))
	}
}

func requireProtoToolResultPart(t *testing.T, part *sigilv1.Part, wantCallID string, wantName string, wantContent string, wantContentJSON []byte, wantIsError bool) {
	t.Helper()

	payload, ok := part.GetPayload().(*sigilv1.Part_ToolResult)
	if !ok {
		t.Fatalf("expected tool result part, got %T", part.GetPayload())
	}
	if got := payload.ToolResult.GetToolCallId(); got != wantCallID {
		t.Fatalf("unexpected tool result tool_call_id: got %q want %q", got, wantCallID)
	}
	if got := payload.ToolResult.GetName(); got != wantName {
		t.Fatalf("unexpected tool result name: got %q want %q", got, wantName)
	}
	if got := payload.ToolResult.GetContent(); got != wantContent {
		t.Fatalf("unexpected tool result content: got %q want %q", got, wantContent)
	}
	if !bytes.Equal(payload.ToolResult.GetContentJson(), wantContentJSON) {
		t.Fatalf("unexpected tool result content_json: got %s want %s", string(payload.ToolResult.GetContentJson()), string(wantContentJSON))
	}
	if got := payload.ToolResult.GetIsError(); got != wantIsError {
		t.Fatalf("unexpected tool result is_error: got %t want %t", got, wantIsError)
	}
}

func requireProtoArtifact(t *testing.T, artifact *sigilv1.Artifact, wantKind sigilv1.ArtifactKind, wantName string, wantContentType string, wantPayload []byte, wantRecordID string, wantURI string) {
	t.Helper()

	if got := artifact.GetKind(); got != wantKind {
		t.Fatalf("unexpected artifact kind: got %v want %v", got, wantKind)
	}
	if got := artifact.GetName(); got != wantName {
		t.Fatalf("unexpected artifact name: got %q want %q", got, wantName)
	}
	if got := artifact.GetContentType(); got != wantContentType {
		t.Fatalf("unexpected artifact content_type: got %q want %q", got, wantContentType)
	}
	if !bytes.Equal(artifact.GetPayload(), wantPayload) {
		t.Fatalf("unexpected artifact payload: got %s want %s", string(artifact.GetPayload()), string(wantPayload))
	}
	if got := artifact.GetRecordId(); got != wantRecordID {
		t.Fatalf("unexpected artifact record_id: got %q want %q", got, wantRecordID)
	}
	if got := artifact.GetUri(); got != wantURI {
		t.Fatalf("unexpected artifact uri: got %q want %q", got, wantURI)
	}
}

func requireInt64HistogramSum(t *testing.T, histogram metricdata.Histogram[int64], attrs map[string]string, want int64) {
	t.Helper()

	point := requireHistogramPointWithAttrs(t, histogram, attrs)
	if point.Sum != want {
		t.Fatalf("unexpected histogram sum for attrs %v: got %d want %d", attrs, point.Sum, want)
	}
	if point.Count != 1 {
		t.Fatalf("unexpected histogram count for attrs %v: got %d want %d", attrs, point.Count, 1)
	}
}
