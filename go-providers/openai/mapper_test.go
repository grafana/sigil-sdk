package openai

import (
	"testing"

	osdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"github.com/grafana/sigil/sdks/go/sigil"
)

func TestFromRequestResponse(t *testing.T) {
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
					"required": []string{"city"},
				},
			}),
		},
	}

	resp := &osdk.ChatCompletion{
		ID:    "chatcmpl_1",
		Model: "gpt-4o-mini",
		Choices: []osdk.ChatCompletionChoice{
			{
				FinishReason: "tool_calls",
				Message: osdk.ChatCompletionMessage{
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

	generation, err := FromRequestResponse(req, resp,
		WithConversationID("conv-9b2f"),
		WithAgentName("agent-openai"),
		WithAgentVersion("v-openai"),
		WithTag("tenant", "t-123"),
	)
	if err != nil {
		t.Fatalf("from request/response: %v", err)
	}

	if generation.Model.Provider != "openai" {
		t.Fatalf("expected provider openai, got %q", generation.Model.Provider)
	}
	if generation.Model.Name != "gpt-4o-mini" {
		t.Fatalf("expected model gpt-4o-mini, got %q", generation.Model.Name)
	}
	if generation.ConversationID != "conv-9b2f" {
		t.Fatalf("expected conv-9b2f, got %q", generation.ConversationID)
	}
	if generation.AgentName != "agent-openai" {
		t.Fatalf("expected agent-openai, got %q", generation.AgentName)
	}
	if generation.AgentVersion != "v-openai" {
		t.Fatalf("expected v-openai, got %q", generation.AgentVersion)
	}
	if generation.ResponseID != "chatcmpl_1" {
		t.Fatalf("expected response id chatcmpl_1, got %q", generation.ResponseID)
	}
	if generation.ResponseModel != "gpt-4o-mini" {
		t.Fatalf("expected response model gpt-4o-mini, got %q", generation.ResponseModel)
	}
	if generation.SystemPrompt != "You are concise." {
		t.Fatalf("unexpected system prompt: %q", generation.SystemPrompt)
	}
	if generation.StopReason != "tool_calls" {
		t.Fatalf("expected stop reason tool_calls, got %q", generation.StopReason)
	}
	if generation.ResponseModel != "gpt-4o-mini" {
		t.Fatalf("expected response model gpt-4o-mini, got %q", generation.ResponseModel)
	}
	if generation.Usage.TotalTokens != 162 {
		t.Fatalf("expected total tokens 162, got %d", generation.Usage.TotalTokens)
	}
	if generation.Usage.CacheReadInputTokens != 8 {
		t.Fatalf("expected cached tokens 8, got %d", generation.Usage.CacheReadInputTokens)
	}
	if generation.Usage.ReasoningTokens != 5 {
		t.Fatalf("expected reasoning tokens 5, got %d", generation.Usage.ReasoningTokens)
	}
	if generation.Tags["tenant"] != "t-123" {
		t.Fatalf("expected tenant tag")
	}
	if len(generation.Artifacts) != 0 {
		t.Fatalf("expected 0 artifacts by default, got %d", len(generation.Artifacts))
	}

	hasToolRole := false
	for _, message := range generation.Input {
		if message.Role == sigil.RoleTool {
			hasToolRole = true
		}
	}
	if !hasToolRole {
		t.Fatalf("expected tool role message from tool result input")
	}
}

func TestFromStream(t *testing.T) {
	req := osdk.ChatCompletionNewParams{
		Model: shared.ChatModel("gpt-4o-mini"),
		Messages: []osdk.ChatCompletionMessageParamUnion{
			osdk.SystemMessage("You are concise."),
			osdk.UserMessage("What is the weather in Paris?"),
		},
		Tools: []osdk.ChatCompletionToolUnionParam{
			osdk.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
				Name: "weather",
				Parameters: shared.FunctionParameters{
					"type": "object",
				},
			}),
		},
	}

	summary := StreamSummary{
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

	generation, err := FromStream(req, summary,
		WithConversationID("conv-stream"),
		WithAgentName("agent-openai-stream"),
		WithAgentVersion("v-openai-stream"),
	)
	if err != nil {
		t.Fatalf("from stream: %v", err)
	}

	if generation.ConversationID != "conv-stream" {
		t.Fatalf("expected conv-stream, got %q", generation.ConversationID)
	}
	if generation.AgentName != "agent-openai-stream" {
		t.Fatalf("expected agent-openai-stream, got %q", generation.AgentName)
	}
	if generation.AgentVersion != "v-openai-stream" {
		t.Fatalf("expected v-openai-stream, got %q", generation.AgentVersion)
	}
	if generation.ResponseID != "chatcmpl_stream_1" {
		t.Fatalf("expected response id chatcmpl_stream_1, got %q", generation.ResponseID)
	}
	if generation.ResponseModel != "gpt-4o-mini" {
		t.Fatalf("expected response model gpt-4o-mini, got %q", generation.ResponseModel)
	}
	if generation.StopReason != "tool_calls" {
		t.Fatalf("expected stop reason tool_calls, got %q", generation.StopReason)
	}
	if generation.Usage.TotalTokens != 25 {
		t.Fatalf("expected total tokens 25, got %d", generation.Usage.TotalTokens)
	}
	if len(generation.Artifacts) != 0 {
		t.Fatalf("expected 0 artifacts by default, got %d", len(generation.Artifacts))
	}
}

func TestFromRequestResponseWithRawArtifacts(t *testing.T) {
	req := osdk.ChatCompletionNewParams{
		Model: shared.ChatModel("gpt-4o-mini"),
		Messages: []osdk.ChatCompletionMessageParamUnion{
			osdk.SystemMessage("You are concise."),
			osdk.UserMessage("hello"),
		},
		Tools: []osdk.ChatCompletionToolUnionParam{
			osdk.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
				Name: "weather",
			}),
		},
	}

	resp := &osdk.ChatCompletion{
		ID:    "chatcmpl_1",
		Model: "gpt-4o-mini",
		Choices: []osdk.ChatCompletionChoice{
			{
				FinishReason: "stop",
				Message: osdk.ChatCompletionMessage{
					Content: "hi",
				},
			},
		},
	}

	generation, err := FromRequestResponse(req, resp, WithRawArtifacts())
	if err != nil {
		t.Fatalf("from request/response: %v", err)
	}

	if len(generation.Artifacts) != 3 {
		t.Fatalf("expected 3 artifacts with raw artifact opt-in, got %d", len(generation.Artifacts))
	}
}
