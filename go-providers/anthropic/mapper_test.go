package anthropic

import (
	"testing"

	asdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/grafana/sigil/sdks/go/sigil"
)

func TestFromRequestResponse(t *testing.T) {
	req := testRequest()
	resp := &asdk.BetaMessage{
		ID:         "msg_1",
		Model:      asdk.Model("claude-sonnet-4-5"),
		StopReason: asdk.BetaStopReasonEndTurn,
		Content: []asdk.BetaContentBlockUnion{
			{Type: "text", Text: "It's 18C and sunny."},
			{Type: "thinking", Thinking: "answer done"},
		},
		Usage: asdk.BetaUsage{
			InputTokens:              120,
			OutputTokens:             42,
			CacheReadInputTokens:     30,
			CacheCreationInputTokens: 10,
			ServerToolUse: asdk.BetaServerToolUsage{
				WebSearchRequests: 2,
				WebFetchRequests:  1,
			},
		},
	}

	generation, err := FromRequestResponse(req, resp,
		WithConversationID("conv-9b2f"),
		WithAgentName("agent-anthropic"),
		WithAgentVersion("v-anthropic"),
		WithTag("tenant", "t-123"),
	)
	if err != nil {
		t.Fatalf("from request/response: %v", err)
	}

	if generation.Model.Provider != "anthropic" {
		t.Fatalf("expected provider anthropic, got %q", generation.Model.Provider)
	}
	if generation.Model.Name != "claude-sonnet-4-5" {
		t.Fatalf("expected model claude-sonnet-4-5, got %q", generation.Model.Name)
	}
	if generation.ConversationID != "conv-9b2f" {
		t.Fatalf("expected conversation id conv-9b2f, got %q", generation.ConversationID)
	}
	if generation.AgentName != "agent-anthropic" {
		t.Fatalf("expected agent-anthropic, got %q", generation.AgentName)
	}
	if generation.AgentVersion != "v-anthropic" {
		t.Fatalf("expected v-anthropic, got %q", generation.AgentVersion)
	}
	if generation.ResponseID != "msg_1" {
		t.Fatalf("expected response id msg_1, got %q", generation.ResponseID)
	}
	if generation.ResponseModel != "claude-sonnet-4-5" {
		t.Fatalf("expected response model claude-sonnet-4-5, got %q", generation.ResponseModel)
	}
	if generation.SystemPrompt != "Be precise." {
		t.Fatalf("unexpected system prompt: %q", generation.SystemPrompt)
	}
	if generation.Usage.TotalTokens != 162 {
		t.Fatalf("expected total tokens 162, got %d", generation.Usage.TotalTokens)
	}
	if generation.Usage.CacheReadInputTokens != 30 {
		t.Fatalf("expected cache read input tokens 30, got %d", generation.Usage.CacheReadInputTokens)
	}
	if generation.Usage.CacheCreationInputTokens != 10 {
		t.Fatalf("expected cache creation tokens 10, got %d", generation.Usage.CacheCreationInputTokens)
	}
	if generation.MaxTokens == nil || *generation.MaxTokens != 512 {
		t.Fatalf("expected max tokens 512, got %v", generation.MaxTokens)
	}
	if generation.Temperature == nil || *generation.Temperature != 0.3 {
		t.Fatalf("expected temperature 0.3, got %v", generation.Temperature)
	}
	if generation.TopP == nil || *generation.TopP != 0.8 {
		t.Fatalf("expected top_p 0.8, got %v", generation.TopP)
	}
	if generation.ToolChoice == nil || *generation.ToolChoice != `{"name":"weather","type":"tool"}` {
		t.Fatalf("unexpected tool choice %v", generation.ToolChoice)
	}
	if generation.ThinkingEnabled == nil || !*generation.ThinkingEnabled {
		t.Fatalf("expected thinking enabled true, got %v", generation.ThinkingEnabled)
	}
	if generation.Metadata == nil {
		t.Fatalf("expected metadata map")
	}
	if generation.Metadata["sigil.gen_ai.request.thinking.budget_tokens"] != int64(1024) {
		t.Fatalf("expected thinking budget metadata 1024, got %v", generation.Metadata["sigil.gen_ai.request.thinking.budget_tokens"])
	}
	if generation.Metadata["sigil.gen_ai.usage.server_tool_use.web_search_requests"] != int64(2) {
		t.Fatalf("expected server tool web_search_requests=2, got %v", generation.Metadata["sigil.gen_ai.usage.server_tool_use.web_search_requests"])
	}
	if generation.Metadata["sigil.gen_ai.usage.server_tool_use.web_fetch_requests"] != int64(1) {
		t.Fatalf("expected server tool web_fetch_requests=1, got %v", generation.Metadata["sigil.gen_ai.usage.server_tool_use.web_fetch_requests"])
	}
	if generation.Metadata["sigil.gen_ai.usage.server_tool_use.total_requests"] != int64(3) {
		t.Fatalf("expected server tool total_requests=3, got %v", generation.Metadata["sigil.gen_ai.usage.server_tool_use.total_requests"])
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
		t.Fatalf("expected mapped tool_result message with tool role")
	}
}

func TestFromStream(t *testing.T) {
	req := testRequest()
	summary := StreamSummary{
		Events: []asdk.BetaRawMessageStreamEventUnion{
			{
				Type: "message_start",
				Message: asdk.BetaMessage{
					ID:    "msg_stream_1",
					Model: asdk.Model("claude-sonnet-4-5"),
				},
			},
			{
				Type: "content_block_start",
				ContentBlock: asdk.BetaRawContentBlockStartEventContentBlockUnion{
					Type:     "thinking",
					Thinking: "look up tool",
				},
			},
			{
				Type: "content_block_start",
				ContentBlock: asdk.BetaRawContentBlockStartEventContentBlockUnion{
					Type:  "tool_use",
					ID:    "toolu_2",
					Name:  "weather",
					Input: map[string]any{"city": "Paris"},
				},
			},
			{
				Type: "content_block_start",
				ContentBlock: asdk.BetaRawContentBlockStartEventContentBlockUnion{
					Type: "text",
					Text: "It's 18C and sunny.",
				},
			},
			{
				Type: "message_delta",
				Delta: asdk.BetaRawMessageStreamEventUnionDelta{
					StopReason: asdk.BetaStopReasonEndTurn,
				},
				Usage: asdk.BetaMessageDeltaUsage{
					InputTokens:              80,
					OutputTokens:             25,
					CacheReadInputTokens:     8,
					CacheCreationInputTokens: 4,
					ServerToolUse: asdk.BetaServerToolUsage{
						WebSearchRequests: 1,
						WebFetchRequests:  2,
					},
				},
			},
		},
	}

	generation, err := FromStream(req, summary,
		WithConversationID("conv-stream"),
		WithAgentName("agent-anthropic-stream"),
		WithAgentVersion("v-anthropic-stream"),
	)
	if err != nil {
		t.Fatalf("from stream: %v", err)
	}

	if generation.ConversationID != "conv-stream" {
		t.Fatalf("expected conv-stream, got %q", generation.ConversationID)
	}
	if generation.AgentName != "agent-anthropic-stream" {
		t.Fatalf("expected agent-anthropic-stream, got %q", generation.AgentName)
	}
	if generation.AgentVersion != "v-anthropic-stream" {
		t.Fatalf("expected v-anthropic-stream, got %q", generation.AgentVersion)
	}
	if generation.ResponseID != "msg_stream_1" {
		t.Fatalf("expected response id msg_stream_1, got %q", generation.ResponseID)
	}
	if generation.StopReason != "end_turn" {
		t.Fatalf("expected end_turn stop reason, got %q", generation.StopReason)
	}
	if generation.ResponseModel != "claude-sonnet-4-5" {
		t.Fatalf("expected response model claude-sonnet-4-5, got %q", generation.ResponseModel)
	}
	if generation.Usage.TotalTokens != 105 {
		t.Fatalf("expected total tokens 105, got %d", generation.Usage.TotalTokens)
	}
	if generation.MaxTokens == nil || *generation.MaxTokens != 512 {
		t.Fatalf("expected max tokens 512, got %v", generation.MaxTokens)
	}
	if generation.Temperature == nil || *generation.Temperature != 0.3 {
		t.Fatalf("expected temperature 0.3, got %v", generation.Temperature)
	}
	if generation.TopP == nil || *generation.TopP != 0.8 {
		t.Fatalf("expected top_p 0.8, got %v", generation.TopP)
	}
	if generation.ToolChoice == nil || *generation.ToolChoice != `{"name":"weather","type":"tool"}` {
		t.Fatalf("unexpected tool choice %v", generation.ToolChoice)
	}
	if generation.ThinkingEnabled == nil || !*generation.ThinkingEnabled {
		t.Fatalf("expected thinking enabled true, got %v", generation.ThinkingEnabled)
	}
	if generation.Metadata == nil {
		t.Fatalf("expected metadata map")
	}
	if generation.Metadata["sigil.gen_ai.request.thinking.budget_tokens"] != int64(1024) {
		t.Fatalf("expected thinking budget metadata 1024, got %v", generation.Metadata["sigil.gen_ai.request.thinking.budget_tokens"])
	}
	if generation.Metadata["sigil.gen_ai.usage.server_tool_use.web_search_requests"] != int64(1) {
		t.Fatalf("expected server tool web_search_requests=1, got %v", generation.Metadata["sigil.gen_ai.usage.server_tool_use.web_search_requests"])
	}
	if generation.Metadata["sigil.gen_ai.usage.server_tool_use.web_fetch_requests"] != int64(2) {
		t.Fatalf("expected server tool web_fetch_requests=2, got %v", generation.Metadata["sigil.gen_ai.usage.server_tool_use.web_fetch_requests"])
	}
	if generation.Metadata["sigil.gen_ai.usage.server_tool_use.total_requests"] != int64(3) {
		t.Fatalf("expected server tool total_requests=3, got %v", generation.Metadata["sigil.gen_ai.usage.server_tool_use.total_requests"])
	}
	if len(generation.Artifacts) != 0 {
		t.Fatalf("expected 0 artifacts by default, got %d", len(generation.Artifacts))
	}
}

func TestFromStreamWithFinalMessageAppendsEventsArtifact(t *testing.T) {
	req := testRequest()
	finalMessage := &asdk.BetaMessage{
		ID:         "msg_final",
		Model:      asdk.Model("claude-sonnet-4-5"),
		StopReason: asdk.BetaStopReasonEndTurn,
		Content: []asdk.BetaContentBlockUnion{
			{Type: "text", Text: "final output"},
		},
		Usage: asdk.BetaUsage{
			InputTokens:  10,
			OutputTokens: 4,
		},
	}

	generation, err := FromStream(req, StreamSummary{
		Events: []asdk.BetaRawMessageStreamEventUnion{
			{Type: "message_stop"},
		},
		FinalMessage: finalMessage,
	})
	if err != nil {
		t.Fatalf("from stream with final message: %v", err)
	}

	if len(generation.Artifacts) != 0 {
		t.Fatalf("expected 0 artifacts by default, got %d", len(generation.Artifacts))
	}
}

func TestFromStreamWithRawArtifacts(t *testing.T) {
	req := testRequest()
	finalMessage := &asdk.BetaMessage{
		ID:         "msg_final",
		Model:      asdk.Model("claude-sonnet-4-5"),
		StopReason: asdk.BetaStopReasonEndTurn,
		Content: []asdk.BetaContentBlockUnion{
			{Type: "text", Text: "final output"},
		},
		Usage: asdk.BetaUsage{
			InputTokens:  10,
			OutputTokens: 4,
		},
	}

	generation, err := FromStream(req, StreamSummary{
		Events: []asdk.BetaRawMessageStreamEventUnion{
			{Type: "message_stop"},
		},
		FinalMessage: finalMessage,
	}, WithRawArtifacts())
	if err != nil {
		t.Fatalf("from stream with final message: %v", err)
	}

	if len(generation.Artifacts) != 4 {
		t.Fatalf("expected 4 artifacts with raw artifact opt-in, got %d", len(generation.Artifacts))
	}
}

func TestFromRequestResponseMapsThinkingDisabled(t *testing.T) {
	req := testRequest()
	disabled := asdk.NewBetaThinkingConfigDisabledParam()
	req.Thinking = asdk.BetaThinkingConfigParamUnion{
		OfDisabled: &disabled,
	}

	resp := &asdk.BetaMessage{
		ID:         "msg_1",
		Model:      asdk.Model("claude-sonnet-4-5"),
		StopReason: asdk.BetaStopReasonEndTurn,
		Content: []asdk.BetaContentBlockUnion{
			{Type: "text", Text: "done"},
		},
	}

	generation, err := FromRequestResponse(req, resp)
	if err != nil {
		t.Fatalf("from request/response: %v", err)
	}

	if generation.ThinkingEnabled == nil || *generation.ThinkingEnabled {
		t.Fatalf("expected thinking enabled false, got %v", generation.ThinkingEnabled)
	}
}

func testRequest() asdk.BetaMessageNewParams {
	toolResult := asdk.NewBetaToolResultBlock("toolu_1")
	toolResult.OfToolResult.Content = []asdk.BetaToolResultBlockParamContentUnion{
		{
			OfText: &asdk.BetaTextBlockParam{
				Text: "18C and sunny",
				Type: "text",
			},
		},
	}

	return asdk.BetaMessageNewParams{
		MaxTokens:   512,
		Model:       asdk.Model("claude-sonnet-4-5"),
		Temperature: param.NewOpt(0.3),
		TopP:        param.NewOpt(0.8),
		ToolChoice:  asdk.BetaToolChoiceParamOfTool("weather"),
		Thinking:    asdk.BetaThinkingConfigParamOfEnabled(1024),
		System: []asdk.BetaTextBlockParam{
			{
				Text: "Be precise.",
				Type: "text",
			},
		},
		Messages: []asdk.BetaMessageParam{
			{
				Role: asdk.BetaMessageParamRoleUser,
				Content: []asdk.BetaContentBlockParamUnion{
					asdk.NewBetaTextBlock("What's the weather in Paris?"),
				},
			},
			{
				Role: asdk.BetaMessageParamRoleAssistant,
				Content: []asdk.BetaContentBlockParamUnion{
					asdk.NewBetaThinkingBlock("sig", "need to call weather tool"),
					asdk.NewBetaToolUseBlock("toolu_1", map[string]any{"city": "Paris"}, "weather"),
				},
			},
			{
				Role: asdk.BetaMessageParamRoleUser,
				Content: []asdk.BetaContentBlockParamUnion{
					toolResult,
				},
			},
		},
		Tools: []asdk.BetaToolUnionParam{
			asdk.BetaToolUnionParamOfTool(asdk.BetaToolInputSchemaParam{
				Type: "object",
				Properties: map[string]any{
					"city": map[string]any{
						"type": "string",
					},
				},
				Required: []string{"city"},
			}, "weather"),
		},
	}
}
