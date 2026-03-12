package openai

import (
	"testing"

	osdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	oresponses "github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"

	"github.com/grafana/sigil/sdks/go/sigil"
)

func TestConformance_ChatCompletionsSyncNormalization(t *testing.T) {
	req := osdk.ChatCompletionNewParams{
		Model: shared.ChatModel("gpt-4o-mini"),
		Messages: []osdk.ChatCompletionMessageParamUnion{
			osdk.SystemMessage("You are concise."),
			osdk.UserMessage("What is the weather in Paris?"),
			osdk.ToolMessage(`{"temp_c":18}`, "call_weather"),
		},
		Tools: []osdk.ChatCompletionToolUnionParam{
			osdk.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
				Name:        "weather",
				Description: osdk.String("Get weather"),
				Parameters: shared.FunctionParameters{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
				},
			}),
		},
		MaxCompletionTokens: param.NewOpt(int64(128)),
		Temperature:         param.NewOpt(0.7),
		TopP:                param.NewOpt(0.9),
		ToolChoice:          osdk.ToolChoiceOptionFunctionToolChoice(osdk.ChatCompletionNamedToolChoiceFunctionParam{Name: "weather"}),
		ReasoningEffort:     shared.ReasoningEffortLow,
	}

	resp := &osdk.ChatCompletion{
		ID:    "chatcmpl_1",
		Model: "gpt-4o-mini",
		Choices: []osdk.ChatCompletionChoice{
			{
				FinishReason: "tool_calls",
				Message: osdk.ChatCompletionMessage{
					Content: "Calling tool",
					ToolCalls: []osdk.ChatCompletionMessageToolCallUnion{
						{
							ID:   "call_weather",
							Type: "function",
							Function: osdk.ChatCompletionMessageFunctionToolCallFunction{
								Name:      "weather",
								Arguments: `{"city":"Paris"}`,
							},
						},
					},
				},
			},
		},
		Usage: osdk.CompletionUsage{
			PromptTokens:     120,
			CompletionTokens: 42,
			TotalTokens:      162,
			PromptTokensDetails: osdk.CompletionUsagePromptTokensDetails{
				CachedTokens: 8,
			},
			CompletionTokensDetails: osdk.CompletionUsageCompletionTokensDetails{
				ReasoningTokens: 5,
			},
		},
	}

	generation, err := ChatCompletionsFromRequestResponse(req, resp,
		WithConversationID("conv-openai-sync"),
		WithConversationTitle("Paris weather"),
		WithAgentName("agent-openai"),
		WithAgentVersion("v-openai"),
		WithTag("tenant", "t-123"),
		WithRawArtifacts(),
	)
	if err != nil {
		t.Fatalf("chat completions sync mapping: %v", err)
	}

	if generation.Model.Provider != "openai" || generation.Model.Name != "gpt-4o-mini" {
		t.Fatalf("unexpected model mapping: %#v", generation.Model)
	}
	if generation.ConversationID != "conv-openai-sync" || generation.ConversationTitle != "Paris weather" {
		t.Fatalf("unexpected conversation mapping: id=%q title=%q", generation.ConversationID, generation.ConversationTitle)
	}
	if generation.AgentName != "agent-openai" || generation.AgentVersion != "v-openai" {
		t.Fatalf("unexpected agent mapping: name=%q version=%q", generation.AgentName, generation.AgentVersion)
	}
	if generation.ResponseID != "chatcmpl_1" || generation.ResponseModel != "gpt-4o-mini" {
		t.Fatalf("unexpected response mapping: id=%q model=%q", generation.ResponseID, generation.ResponseModel)
	}
	if generation.SystemPrompt != "You are concise." {
		t.Fatalf("unexpected system prompt: %q", generation.SystemPrompt)
	}
	if generation.StopReason != "tool_calls" {
		t.Fatalf("unexpected stop reason: %q", generation.StopReason)
	}
	if generation.Usage.TotalTokens != 162 || generation.Usage.CacheReadInputTokens != 8 || generation.Usage.ReasoningTokens != 5 {
		t.Fatalf("unexpected usage mapping: %#v", generation.Usage)
	}
	if generation.ThinkingEnabled == nil || !*generation.ThinkingEnabled {
		t.Fatalf("expected thinking enabled true, got %v", generation.ThinkingEnabled)
	}
	if len(generation.Output) != 1 || len(generation.Output[0].Parts) != 2 {
		t.Fatalf("expected one assistant message with text + tool call, got %#v", generation.Output)
	}
	if generation.Output[0].Parts[0].Kind != sigil.PartKindText || generation.Output[0].Parts[0].Text != "Calling tool" {
		t.Fatalf("unexpected assistant text part: %#v", generation.Output[0].Parts[0])
	}
	if generation.Output[0].Parts[1].Kind != sigil.PartKindToolCall {
		t.Fatalf("expected tool_call part, got %#v", generation.Output[0].Parts[1])
	}
	if generation.Output[0].Parts[1].ToolCall.ID != "call_weather" || generation.Output[0].Parts[1].ToolCall.Name != "weather" {
		t.Fatalf("unexpected tool call mapping: %#v", generation.Output[0].Parts[1].ToolCall)
	}
	if string(generation.Output[0].Parts[1].ToolCall.InputJSON) != `{"city":"Paris"}` {
		t.Fatalf("unexpected tool call input: %q", string(generation.Output[0].Parts[1].ToolCall.InputJSON))
	}
	if generation.Tags["tenant"] != "t-123" {
		t.Fatalf("expected tenant tag")
	}
	requireOpenAIArtifactKinds(t, generation.Artifacts,
		sigil.ArtifactKindRequest,
		sigil.ArtifactKindResponse,
		sigil.ArtifactKindTools,
	)
}

func TestConformance_ChatCompletionsStreamNormalization(t *testing.T) {
	req := osdk.ChatCompletionNewParams{
		Model: shared.ChatModel("gpt-4o-mini"),
		Messages: []osdk.ChatCompletionMessageParamUnion{
			osdk.SystemMessage("You are concise."),
			osdk.UserMessage("What is the weather in Paris?"),
		},
		Tools: []osdk.ChatCompletionToolUnionParam{
			osdk.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
				Name: "weather",
			}),
		},
		MaxCompletionTokens: param.NewOpt(int64(42)),
		Temperature:         param.NewOpt(0.15),
		TopP:                param.NewOpt(0.4),
		ToolChoice:          osdk.ToolChoiceOptionFunctionToolChoice(osdk.ChatCompletionNamedToolChoiceFunctionParam{Name: "weather"}),
		ReasoningEffort:     shared.ReasoningEffortMedium,
	}

	summary := ChatCompletionsStreamSummary{
		Chunks: []osdk.ChatCompletionChunk{
			{
				ID:    "chatcmpl_stream_1",
				Model: "gpt-4o-mini",
				Choices: []osdk.ChatCompletionChunkChoice{
					{
						Delta: osdk.ChatCompletionChunkChoiceDelta{
							Content: "Calling tool",
							ToolCalls: []osdk.ChatCompletionChunkChoiceDeltaToolCall{
								{
									Index: 0,
									ID:    "call_weather",
									Function: osdk.ChatCompletionChunkChoiceDeltaToolCallFunction{
										Name:      "weather",
										Arguments: `{"city":"Pa`,
									},
								},
							},
						},
					},
				},
			},
			{
				Choices: []osdk.ChatCompletionChunkChoice{
					{
						Delta: osdk.ChatCompletionChunkChoiceDelta{
							Content: " now.",
							ToolCalls: []osdk.ChatCompletionChunkChoiceDeltaToolCall{
								{
									Index: 0,
									Function: osdk.ChatCompletionChunkChoiceDeltaToolCallFunction{
										Arguments: `ris"}`,
									},
								},
							},
						},
						FinishReason: "tool_calls",
					},
				},
				Usage: osdk.CompletionUsage{
					PromptTokens:     20,
					CompletionTokens: 5,
					TotalTokens:      25,
				},
			},
		},
	}

	generation, err := ChatCompletionsFromStream(req, summary,
		WithConversationID("conv-openai-stream"),
		WithAgentName("agent-openai-stream"),
		WithAgentVersion("v-openai-stream"),
		WithRawArtifacts(),
	)
	if err != nil {
		t.Fatalf("chat completions stream mapping: %v", err)
	}

	if generation.ConversationID != "conv-openai-stream" || generation.AgentName != "agent-openai-stream" || generation.AgentVersion != "v-openai-stream" {
		t.Fatalf("unexpected identity mapping: %#v", generation)
	}
	if generation.ResponseID != "chatcmpl_stream_1" || generation.ResponseModel != "gpt-4o-mini" {
		t.Fatalf("unexpected response mapping: id=%q model=%q", generation.ResponseID, generation.ResponseModel)
	}
	if generation.StopReason != "tool_calls" {
		t.Fatalf("unexpected stop reason: %q", generation.StopReason)
	}
	if generation.Usage.TotalTokens != 25 {
		t.Fatalf("unexpected usage mapping: %#v", generation.Usage)
	}
	if generation.ThinkingEnabled == nil || !*generation.ThinkingEnabled {
		t.Fatalf("expected thinking enabled true, got %v", generation.ThinkingEnabled)
	}
	if len(generation.Output) != 1 || len(generation.Output[0].Parts) != 2 {
		t.Fatalf("expected merged assistant output, got %#v", generation.Output)
	}
	if generation.Output[0].Parts[0].Text != "Calling tool now." {
		t.Fatalf("unexpected streamed text: %q", generation.Output[0].Parts[0].Text)
	}
	if generation.Output[0].Parts[1].Kind != sigil.PartKindToolCall {
		t.Fatalf("expected tool call output, got %#v", generation.Output[0].Parts[1])
	}
	if string(generation.Output[0].Parts[1].ToolCall.InputJSON) != `{"city":"Paris"}` {
		t.Fatalf("unexpected streamed tool input: %q", string(generation.Output[0].Parts[1].ToolCall.InputJSON))
	}
	requireOpenAIArtifactKinds(t, generation.Artifacts,
		sigil.ArtifactKindRequest,
		sigil.ArtifactKindTools,
		sigil.ArtifactKindProviderEvent,
	)
}

func TestConformance_ResponsesSyncNormalization(t *testing.T) {
	req := oresponses.ResponseNewParams{
		Model:           shared.ResponsesModel("gpt-5"),
		Instructions:    param.NewOpt("Be concise."),
		Input:           oresponses.ResponseNewParamsInputUnion{OfString: param.NewOpt("hello")},
		MaxOutputTokens: param.NewOpt(int64(320)),
		Temperature:     param.NewOpt(0.2),
		TopP:            param.NewOpt(0.85),
		Reasoning: shared.ReasoningParam{
			Effort: shared.ReasoningEffortMedium,
		},
	}

	resp := &oresponses.Response{
		ID:     "resp_1",
		Model:  shared.ResponsesModel("gpt-5"),
		Status: oresponses.ResponseStatusCompleted,
		Output: []oresponses.ResponseOutputItemUnion{
			{
				Type: "message",
				Content: []oresponses.ResponseOutputMessageContentUnion{
					{Type: "output_text", Text: "world"},
				},
			},
			{
				Type:      "function_call",
				CallID:    "call_weather",
				Name:      "weather",
				Arguments: oresponses.ResponseOutputItemUnionArguments{OfString: `{"city":"Paris"}`},
			},
		},
		Usage: oresponses.ResponseUsage{
			InputTokens:  80,
			OutputTokens: 20,
			TotalTokens:  100,
			InputTokensDetails: oresponses.ResponseUsageInputTokensDetails{
				CachedTokens: 2,
			},
			OutputTokensDetails: oresponses.ResponseUsageOutputTokensDetails{
				ReasoningTokens: 3,
			},
		},
	}

	generation, err := ResponsesFromRequestResponse(req, resp, WithRawArtifacts())
	if err != nil {
		t.Fatalf("responses sync mapping: %v", err)
	}

	if generation.Model.Provider != "openai" || generation.Model.Name != "gpt-5" {
		t.Fatalf("unexpected model mapping: %#v", generation.Model)
	}
	if generation.ResponseID != "resp_1" || generation.ResponseModel != "gpt-5" {
		t.Fatalf("unexpected response mapping: id=%q model=%q", generation.ResponseID, generation.ResponseModel)
	}
	if generation.SystemPrompt != "Be concise." {
		t.Fatalf("unexpected system prompt: %q", generation.SystemPrompt)
	}
	if generation.StopReason != "stop" {
		t.Fatalf("unexpected stop reason: %q", generation.StopReason)
	}
	if generation.ThinkingEnabled == nil || !*generation.ThinkingEnabled {
		t.Fatalf("expected thinking enabled true, got %v", generation.ThinkingEnabled)
	}
	if generation.Usage.TotalTokens != 100 || generation.Usage.CacheReadInputTokens != 2 || generation.Usage.ReasoningTokens != 3 {
		t.Fatalf("unexpected usage mapping: %#v", generation.Usage)
	}
	if len(generation.Output) != 2 {
		t.Fatalf("expected text + tool call outputs, got %#v", generation.Output)
	}
	if generation.Output[0].Parts[0].Text != "world" {
		t.Fatalf("unexpected response text: %q", generation.Output[0].Parts[0].Text)
	}
	if generation.Output[1].Parts[0].Kind != sigil.PartKindToolCall {
		t.Fatalf("expected response tool call, got %#v", generation.Output[1].Parts[0])
	}
	requireOpenAIArtifactKinds(t, generation.Artifacts,
		sigil.ArtifactKindRequest,
		sigil.ArtifactKindResponse,
	)
}

func TestConformance_ResponsesStreamNormalization(t *testing.T) {
	req := oresponses.ResponseNewParams{
		Model:           shared.ResponsesModel("gpt-5"),
		Input:           oresponses.ResponseNewParamsInputUnion{OfString: param.NewOpt("hello")},
		MaxOutputTokens: param.NewOpt(int64(128)),
	}

	summary := ResponsesStreamSummary{
		Events: []oresponses.ResponseStreamEventUnion{
			{
				Type:  "response.output_text.delta",
				Delta: "hello",
			},
			{
				Type:  "response.output_text.delta",
				Delta: " world",
			},
			{
				Type: "response.completed",
				Response: oresponses.Response{
					ID:    "resp_stream_1",
					Model: shared.ResponsesModel("gpt-5"),
				},
			},
		},
	}

	generation, err := ResponsesFromStream(req, summary, WithRawArtifacts())
	if err != nil {
		t.Fatalf("responses stream mapping: %v", err)
	}

	if generation.Model.Provider != "openai" || generation.Model.Name != "gpt-5" {
		t.Fatalf("unexpected model mapping: %#v", generation.Model)
	}
	if generation.ResponseID != "resp_stream_1" || generation.ResponseModel != "gpt-5" {
		t.Fatalf("unexpected response mapping: id=%q model=%q", generation.ResponseID, generation.ResponseModel)
	}
	if generation.StopReason != "stop" {
		t.Fatalf("unexpected stop reason: %q", generation.StopReason)
	}
	if len(generation.Output) != 1 || generation.Output[0].Parts[0].Text != "hello world" {
		t.Fatalf("unexpected streamed output: %#v", generation.Output)
	}
	requireOpenAIArtifactKinds(t, generation.Artifacts,
		sigil.ArtifactKindRequest,
		sigil.ArtifactKindProviderEvent,
	)
}

func TestConformance_OpenAIErrorMapping(t *testing.T) {
	if _, err := ChatCompletionsFromRequestResponse(osdk.ChatCompletionNewParams{}, nil); err == nil || err.Error() != "response is required" {
		t.Fatalf("expected explicit chat response error, got %v", err)
	}
	if _, err := ChatCompletionsFromStream(osdk.ChatCompletionNewParams{}, ChatCompletionsStreamSummary{}); err == nil || err.Error() != "stream summary has no chunks and no final response" {
		t.Fatalf("expected explicit chat stream error, got %v", err)
	}
	if _, err := ResponsesFromRequestResponse(oresponses.ResponseNewParams{}, nil); err == nil || err.Error() != "response is required" {
		t.Fatalf("expected explicit responses response error, got %v", err)
	}
	if _, err := ResponsesFromStream(oresponses.ResponseNewParams{}, ResponsesStreamSummary{}); err == nil || err.Error() != "stream summary has no events and no final response" {
		t.Fatalf("expected explicit responses stream error, got %v", err)
	}

	_, err := ChatCompletionsFromRequestResponse(
		osdk.ChatCompletionNewParams{Model: shared.ChatModel("gpt-4o-mini")},
		&osdk.ChatCompletion{Model: "gpt-4o-mini"},
		WithProviderName(""),
	)
	if err == nil || err.Error() != "generation.model.provider is required" {
		t.Fatalf("expected explicit validation error for invalid provider mapping, got %v", err)
	}
}

func requireOpenAIArtifactKinds(t *testing.T, artifacts []sigil.Artifact, want ...sigil.ArtifactKind) {
	t.Helper()

	if len(artifacts) != len(want) {
		t.Fatalf("expected %d artifacts, got %d", len(want), len(artifacts))
	}
	for i, kind := range want {
		if artifacts[i].Kind != kind {
			t.Fatalf("artifact %d kind mismatch: got %q want %q", i, artifacts[i].Kind, kind)
		}
	}
}
