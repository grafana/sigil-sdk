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

	startedAt := time.Date(2026, 3, 12, 8, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(2 * time.Second)
	maxTokens := int64(256)
	temperature := 0.25
	topP := 0.9
	toolChoice := "required"
	thinkingEnabled := true
	toolSchema := json.RawMessage(`{"type":"object","properties":{"location":{"type":"string"}},"required":["location"]}`)
	toolCallInput := json.RawMessage(`{"location":"Paris"}`)
	toolResultContent := json.RawMessage(`{"forecast":"sunny","temp_c":22}`)

	requestArtifact, err := sigil.NewJSONArtifact(sigil.ArtifactKindRequest, "request", map[string]any{
		"model": "gpt-5",
	})
	if err != nil {
		t.Fatalf("new request artifact: %v", err)
	}

	responseArtifact, err := sigil.NewJSONArtifact(sigil.ArtifactKindResponse, "response", map[string]any{
		"stop_reason": "end_turn",
	})
	if err != nil {
		t.Fatalf("new response artifact: %v", err)
	}

	parentCtx, parent := env.tracerProvider.Tracer("sigil-conformance-parent").Start(context.Background(), "parent")
	parentSC := parent.SpanContext()

	callCtx, recorder := env.Client.StartGeneration(parentCtx, sigil.GenerationStart{
		ID:                "gen-roundtrip-1",
		ConversationID:    "conv-roundtrip-1",
		ConversationTitle: "Ticket triage",
		UserID:            "user-42",
		AgentName:         "agent-support",
		AgentVersion:      "v1.2.3",
		Model:             conformanceModel,
		SystemPrompt:      "You are a concise support assistant.",
		Tools: []sigil.ToolDefinition{{
			Name:        "lookup_weather",
			Description: "Look up the latest weather conditions",
			Type:        "function",
			InputSchema: toolSchema,
			Deferred:    true,
		}},
		MaxTokens:       &maxTokens,
		Temperature:     &temperature,
		TopP:            &topP,
		ToolChoice:      &toolChoice,
		ThinkingEnabled: &thinkingEnabled,
		Tags: map[string]string{
			"suite": "conformance",
		},
		Metadata: map[string]any{
			"request_id":              "req-7",
			metadataKeyThinkingBudget: int64(4096),
		},
		StartedAt: startedAt,
	})
	callSC := trace.SpanContextFromContext(callCtx)
	if !callSC.IsValid() {
		t.Fatalf("expected valid generation span context")
	}

	recorder.SetResult(sigil.Generation{
		ResponseID:    "resp-7",
		ResponseModel: "gpt-5-2026-03-01",
		Input: []sigil.Message{{
			Role:  sigil.RoleUser,
			Name:  "customer",
			Parts: []sigil.Part{sigil.TextPart("What's the weather in Paris?")},
		}},
		Output: []sigil.Message{
			{
				Role: sigil.RoleAssistant,
				Parts: []sigil.Part{
					sigil.ThinkingPart("I have the tool result; compose the final answer."),
					sigil.ToolCallPart(sigil.ToolCall{
						ID:        "call-1",
						Name:      "lookup_weather",
						InputJSON: toolCallInput,
					}),
					sigil.TextPart("It is sunny and 22C in Paris."),
				},
			},
			{
				Role: sigil.RoleTool,
				Name: "lookup_weather",
				Parts: []sigil.Part{sigil.ToolResultPart(sigil.ToolResult{
					ToolCallID:  "call-1",
					Name:        "lookup_weather",
					Content:     "sunny, 22C",
					ContentJSON: toolResultContent,
				})},
			},
			sigil.AssistantTextMessage("It is sunny and 22C in Paris."),
		},
		Tags: map[string]string{
			"scenario": "full-roundtrip",
		},
		Metadata: map[string]any{
			"response_format": "text",
		},
		Artifacts: []sigil.Artifact{requestArtifact, responseArtifact},
		Usage: sigil.TokenUsage{
			InputTokens:              120,
			OutputTokens:             36,
			CacheCreationInputTokens: 4,
			CacheReadInputTokens:     5,
			CacheWriteInputTokens:    3,
			ReasoningTokens:          7,
		},
		StopReason:  "end_turn",
		CompletedAt: completedAt,
	}, nil)
	recorder.End()
	if err := recorder.Err(); err != nil {
		t.Fatalf("record generation: %v", err)
	}

	parent.End()

	span := findSpan(t, env.Spans.Ended(), conformanceOperationName)
	if got := span.Name(); got != "generateText gpt-5" {
		t.Fatalf("unexpected span name: got %q want %q", got, "generateText gpt-5")
	}
	if got := span.SpanKind(); got != trace.SpanKindClient {
		t.Fatalf("unexpected span kind: got %v want %v", got, trace.SpanKindClient)
	}
	if span.SpanContext().TraceID() != callSC.TraceID() {
		t.Fatalf("unexpected span trace id: got %q want %q", span.SpanContext().TraceID(), callSC.TraceID())
	}
	if span.SpanContext().SpanID() != callSC.SpanID() {
		t.Fatalf("unexpected span span id: got %q want %q", span.SpanContext().SpanID(), callSC.SpanID())
	}
	if span.Parent().SpanID() != parentSC.SpanID() {
		t.Fatalf("unexpected parent span id: got %q want %q", span.Parent().SpanID(), parentSC.SpanID())
	}
	if got := span.Status().Code; got != codes.Ok {
		t.Fatalf("unexpected span status: got %v want %v", got, codes.Ok)
	}

	attrs := spanAttrs(span)
	requireSpanAttr(t, attrs, spanAttrOperationName, conformanceOperationName)
	requireSpanAttr(t, attrs, spanAttrGenerationID, "gen-roundtrip-1")
	requireSpanAttr(t, attrs, spanAttrConversationID, "conv-roundtrip-1")
	requireSpanAttr(t, attrs, spanAttrConversationTitle, "Ticket triage")
	requireSpanAttr(t, attrs, spanAttrUserID, "user-42")
	requireSpanAttr(t, attrs, spanAttrAgentName, "agent-support")
	requireSpanAttr(t, attrs, spanAttrAgentVersion, "v1.2.3")
	requireSpanAttr(t, attrs, spanAttrProviderName, conformanceModel.Provider)
	requireSpanAttr(t, attrs, spanAttrRequestModel, conformanceModel.Name)
	requireSpanAttr(t, attrs, spanAttrResponseID, "resp-7")
	requireSpanAttr(t, attrs, spanAttrResponseModel, "gpt-5-2026-03-01")
	requireSpanAttrInt64(t, attrs, spanAttrRequestMaxTokens, maxTokens)
	requireSpanAttrFloat64(t, attrs, spanAttrRequestTemperature, temperature)
	requireSpanAttrFloat64(t, attrs, spanAttrRequestTopP, topP)
	requireSpanAttr(t, attrs, spanAttrRequestToolChoice, toolChoice)
	requireSpanAttrBool(t, attrs, spanAttrRequestThinkingEnabled, thinkingEnabled)
	requireSpanAttrInt64(t, attrs, metadataKeyThinkingBudget, 4096)
	requireSpanAttrStringSlice(t, attrs, spanAttrFinishReasons, []string{"end_turn"})
	requireSpanAttrInt64(t, attrs, spanAttrInputTokens, 120)
	requireSpanAttrInt64(t, attrs, spanAttrOutputTokens, 36)
	requireSpanAttrInt64(t, attrs, spanAttrCacheReadTokens, 5)
	requireSpanAttrInt64(t, attrs, spanAttrCacheWriteTokens, 3)
	requireSpanAttrInt64(t, attrs, spanAttrCacheCreationTokens, 4)
	requireSpanAttrInt64(t, attrs, spanAttrReasoningTokens, 7)
	requireSpanAttr(t, attrs, metadataKeySDKName, sdkNameGo)

	metrics := env.CollectMetrics(t)
	duration := findHistogram[float64](t, metrics, metricOperationDuration)
	durationPoint := requireHistogramPointWithAttrs(t, duration, map[string]string{
		spanAttrOperationName: conformanceOperationName,
		spanAttrProviderName:  conformanceModel.Provider,
		spanAttrRequestModel:  conformanceModel.Name,
		spanAttrAgentName:     "agent-support",
		spanAttrErrorType:     "",
		spanAttrErrorCategory: "",
	})
	if durationPoint.Sum != completedAt.Sub(startedAt).Seconds() {
		t.Fatalf("unexpected %s sum: got %v want %v", metricOperationDuration, durationPoint.Sum, completedAt.Sub(startedAt).Seconds())
	}
	if durationPoint.Count != 1 {
		t.Fatalf("unexpected %s count: got %d want %d", metricOperationDuration, durationPoint.Count, 1)
	}

	tokenUsage := findHistogram[int64](t, metrics, metricTokenUsage)
	for tokenType, value := range map[string]int64{
		metricTokenTypeInput:         120,
		metricTokenTypeOutput:        36,
		metricTokenTypeCacheRead:     5,
		metricTokenTypeCacheWrite:    3,
		metricTokenTypeCacheCreation: 4,
		metricTokenTypeReasoning:     7,
	} {
		requireInt64HistogramSum(t, tokenUsage, map[string]string{
			spanAttrOperationName: conformanceOperationName,
			spanAttrProviderName:  conformanceModel.Provider,
			spanAttrRequestModel:  conformanceModel.Name,
			spanAttrAgentName:     "agent-support",
			metricAttrTokenType:   tokenType,
		}, value)
	}

	toolCalls := findHistogram[int64](t, metrics, metricToolCallsPerOperation)
	requireInt64HistogramSum(t, toolCalls, map[string]string{
		spanAttrProviderName: conformanceModel.Provider,
		spanAttrRequestModel: conformanceModel.Name,
		spanAttrAgentName:    "agent-support",
	}, 1)
	requireNoHistogram(t, metrics, metricTimeToFirstToken)

	env.Shutdown(t)

	generation := env.Ingest.SingleGeneration(t)
	if got := generation.GetId(); got != "gen-roundtrip-1" {
		t.Fatalf("unexpected generation id: got %q want %q", got, "gen-roundtrip-1")
	}
	if got := generation.GetConversationId(); got != "conv-roundtrip-1" {
		t.Fatalf("unexpected conversation id: got %q want %q", got, "conv-roundtrip-1")
	}
	if got := generation.GetOperationName(); got != conformanceOperationName {
		t.Fatalf("unexpected operation name: got %q want %q", got, conformanceOperationName)
	}
	if got := generation.GetMode(); got != sigilv1.GenerationMode_GENERATION_MODE_SYNC {
		t.Fatalf("unexpected generation mode: got %v want %v", got, sigilv1.GenerationMode_GENERATION_MODE_SYNC)
	}
	if got := generation.GetAgentName(); got != "agent-support" {
		t.Fatalf("unexpected agent name: got %q want %q", got, "agent-support")
	}
	if got := generation.GetAgentVersion(); got != "v1.2.3" {
		t.Fatalf("unexpected agent version: got %q want %q", got, "v1.2.3")
	}
	if got := generation.GetTraceId(); got != callSC.TraceID().String() {
		t.Fatalf("unexpected trace id: got %q want %q", got, callSC.TraceID().String())
	}
	if got := generation.GetSpanId(); got != callSC.SpanID().String() {
		t.Fatalf("unexpected span id: got %q want %q", got, callSC.SpanID().String())
	}
	if got := generation.GetResponseId(); got != "resp-7" {
		t.Fatalf("unexpected response id: got %q want %q", got, "resp-7")
	}
	if got := generation.GetResponseModel(); got != "gpt-5-2026-03-01" {
		t.Fatalf("unexpected response model: got %q want %q", got, "gpt-5-2026-03-01")
	}
	if got := generation.GetSystemPrompt(); got != "You are a concise support assistant." {
		t.Fatalf("unexpected system prompt: got %q want %q", got, "You are a concise support assistant.")
	}
	if got := generation.GetStopReason(); got != "end_turn" {
		t.Fatalf("unexpected stop reason: got %q want %q", got, "end_turn")
	}
	if !generation.GetStartedAt().AsTime().Equal(startedAt) {
		t.Fatalf("unexpected started_at: got %s want %s", generation.GetStartedAt().AsTime(), startedAt)
	}
	if !generation.GetCompletedAt().AsTime().Equal(completedAt) {
		t.Fatalf("unexpected completed_at: got %s want %s", generation.GetCompletedAt().AsTime(), completedAt)
	}
	if got := generation.GetModel().GetProvider(); got != conformanceModel.Provider {
		t.Fatalf("unexpected model provider: got %q want %q", got, conformanceModel.Provider)
	}
	if got := generation.GetModel().GetName(); got != conformanceModel.Name {
		t.Fatalf("unexpected model name: got %q want %q", got, conformanceModel.Name)
	}
	if got := generation.GetMaxTokens(); got != maxTokens {
		t.Fatalf("unexpected max_tokens: got %d want %d", got, maxTokens)
	}
	if got := generation.GetTemperature(); got != temperature {
		t.Fatalf("unexpected temperature: got %v want %v", got, temperature)
	}
	if got := generation.GetTopP(); got != topP {
		t.Fatalf("unexpected top_p: got %v want %v", got, topP)
	}
	if got := generation.GetToolChoice(); got != toolChoice {
		t.Fatalf("unexpected tool_choice: got %q want %q", got, toolChoice)
	}
	if got := generation.GetThinkingEnabled(); got != thinkingEnabled {
		t.Fatalf("unexpected thinking_enabled: got %t want %t", got, thinkingEnabled)
	}

	requireProtoMetadata(t, generation, metadataKeyConversation, "Ticket triage")
	requireProtoMetadata(t, generation, metadataKeyCanonicalUserID, "user-42")
	requireProtoMetadata(t, generation, metadataKeySDKName, sdkNameGo)
	requireProtoMetadata(t, generation, "request_id", "req-7")
	requireProtoMetadata(t, generation, "response_format", "text")
	requireProtoMetadataNumber(t, generation, metadataKeyThinkingBudget, 4096)

	if len(generation.GetTags()) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(generation.GetTags()))
	}
	if got := generation.GetTags()["suite"]; got != "conformance" {
		t.Fatalf("unexpected suite tag: got %q want %q", got, "conformance")
	}
	if got := generation.GetTags()["scenario"]; got != "full-roundtrip" {
		t.Fatalf("unexpected scenario tag: got %q want %q", got, "full-roundtrip")
	}

	tools := generation.GetTools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if got := tools[0].GetName(); got != "lookup_weather" {
		t.Fatalf("unexpected tool name: got %q want %q", got, "lookup_weather")
	}
	if got := tools[0].GetDescription(); got != "Look up the latest weather conditions" {
		t.Fatalf("unexpected tool description: got %q want %q", got, "Look up the latest weather conditions")
	}
	if got := tools[0].GetType(); got != "function" {
		t.Fatalf("unexpected tool type: got %q want %q", got, "function")
	}
	if !bytes.Equal(tools[0].GetInputSchemaJson(), toolSchema) {
		t.Fatalf("unexpected tool input schema: got %s want %s", string(tools[0].GetInputSchemaJson()), string(toolSchema))
	}
	if got := tools[0].GetDeferred(); !got {
		t.Fatalf("expected deferred tool definition")
	}

	input := generation.GetInput()
	if len(input) != 1 {
		t.Fatalf("expected 1 input message, got %d", len(input))
	}
	if got := input[0].GetRole(); got != sigilv1.MessageRole_MESSAGE_ROLE_USER {
		t.Fatalf("unexpected input role: got %v want %v", got, sigilv1.MessageRole_MESSAGE_ROLE_USER)
	}
	if got := input[0].GetName(); got != "customer" {
		t.Fatalf("unexpected input name: got %q want %q", got, "customer")
	}
	requireProtoTextPart(t, input[0].GetParts()[0], "What's the weather in Paris?")

	output := generation.GetOutput()
	if len(output) != 3 {
		t.Fatalf("expected 3 output messages, got %d", len(output))
	}
	if got := output[0].GetRole(); got != sigilv1.MessageRole_MESSAGE_ROLE_ASSISTANT {
		t.Fatalf("unexpected output[0] role: got %v want %v", got, sigilv1.MessageRole_MESSAGE_ROLE_ASSISTANT)
	}
	if len(output[0].GetParts()) != 3 {
		t.Fatalf("unexpected output[0] part count: got %d want %d", len(output[0].GetParts()), 3)
	}
	requireProtoThinkingPart(t, output[0].GetParts()[0], "I have the tool result; compose the final answer.")
	requireProtoToolCallPart(t, output[0].GetParts()[1], "call-1", "lookup_weather", toolCallInput)
	requireProtoTextPart(t, output[0].GetParts()[2], "It is sunny and 22C in Paris.")

	if got := output[1].GetRole(); got != sigilv1.MessageRole_MESSAGE_ROLE_TOOL {
		t.Fatalf("unexpected output[1] role: got %v want %v", got, sigilv1.MessageRole_MESSAGE_ROLE_TOOL)
	}
	if got := output[1].GetName(); got != "lookup_weather" {
		t.Fatalf("unexpected output[1] name: got %q want %q", got, "lookup_weather")
	}
	requireProtoToolResultPart(t, output[1].GetParts()[0], "call-1", "lookup_weather", "sunny, 22C", toolResultContent, false)

	if got := output[2].GetRole(); got != sigilv1.MessageRole_MESSAGE_ROLE_ASSISTANT {
		t.Fatalf("unexpected output[2] role: got %v want %v", got, sigilv1.MessageRole_MESSAGE_ROLE_ASSISTANT)
	}
	requireProtoTextPart(t, output[2].GetParts()[0], "It is sunny and 22C in Paris.")

	usage := generation.GetUsage()
	if got := usage.GetInputTokens(); got != 120 {
		t.Fatalf("unexpected input tokens: got %d want %d", got, 120)
	}
	if got := usage.GetOutputTokens(); got != 36 {
		t.Fatalf("unexpected output tokens: got %d want %d", got, 36)
	}
	if got := usage.GetTotalTokens(); got != 156 {
		t.Fatalf("unexpected total tokens: got %d want %d", got, 156)
	}
	if got := usage.GetCacheReadInputTokens(); got != 5 {
		t.Fatalf("unexpected cache read tokens: got %d want %d", got, 5)
	}
	if got := usage.GetCacheWriteInputTokens(); got != 3 {
		t.Fatalf("unexpected cache write tokens: got %d want %d", got, 3)
	}
	if got := usage.GetCacheCreationInputTokens(); got != 4 {
		t.Fatalf("unexpected cache creation tokens: got %d want %d", got, 4)
	}
	if got := usage.GetReasoningTokens(); got != 7 {
		t.Fatalf("unexpected reasoning tokens: got %d want %d", got, 7)
	}
	if got := generation.GetCallError(); got != "" {
		t.Fatalf("expected empty call error, got %q", got)
	}

	artifacts := generation.GetRawArtifacts()
	if len(artifacts) != 2 {
		t.Fatalf("expected 2 raw artifacts, got %d", len(artifacts))
	}
	requireProtoArtifact(t, artifacts[0], sigilv1.ArtifactKind_ARTIFACT_KIND_REQUEST, "request", "application/json", []byte(`{"model":"gpt-5"}`), "", "")
	requireProtoArtifact(t, artifacts[1], sigilv1.ArtifactKind_ARTIFACT_KIND_RESPONSE, "response", "application/json", []byte(`{"stop_reason":"end_turn"}`), "", "")
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
