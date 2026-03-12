package anthropic

import (
	"testing"

	asdk "github.com/anthropics/anthropic-sdk-go"

	"github.com/grafana/sigil/sdks/go/sigil"
)

func TestConformance_MessageSyncNormalization(t *testing.T) {
	req := testRequest()
	resp := &asdk.BetaMessage{
		ID:         "msg_1",
		Model:      asdk.Model("claude-sonnet-4-5"),
		StopReason: asdk.BetaStopReasonEndTurn,
		Content: []asdk.BetaContentBlockUnion{
			{Type: "text", Text: "It's 18C and sunny."},
			{Type: "thinking", Thinking: "answer done"},
			mustUnmarshalBetaContentBlockUnion(t, `{"type":"tool_use","id":"toolu_2","name":"weather","input":{"city":"Paris"}}`),
		},
		Usage: asdk.BetaUsage{
			InputTokens:              120,
			OutputTokens:             42,
			CacheReadInputTokens:     30,
			CacheCreationInputTokens: 10,
		},
	}

	generation, err := FromRequestResponse(req, resp,
		WithConversationID("conv-anthropic-sync"),
		WithConversationTitle("Paris weather"),
		WithAgentName("agent-anthropic"),
		WithAgentVersion("v-anthropic"),
		WithTag("tenant", "t-123"),
		WithRawArtifacts(),
	)
	if err != nil {
		t.Fatalf("anthropic sync mapping: %v", err)
	}

	if generation.Model.Provider != "anthropic" || generation.Model.Name != "claude-sonnet-4-5" {
		t.Fatalf("unexpected model mapping: %#v", generation.Model)
	}
	if generation.ConversationID != "conv-anthropic-sync" || generation.ConversationTitle != "Paris weather" {
		t.Fatalf("unexpected conversation mapping: %#v", generation)
	}
	if generation.AgentName != "agent-anthropic" || generation.AgentVersion != "v-anthropic" {
		t.Fatalf("unexpected agent mapping: name=%q version=%q", generation.AgentName, generation.AgentVersion)
	}
	if generation.ResponseID != "msg_1" || generation.ResponseModel != "claude-sonnet-4-5" {
		t.Fatalf("unexpected response mapping: id=%q model=%q", generation.ResponseID, generation.ResponseModel)
	}
	if generation.StopReason != "end_turn" {
		t.Fatalf("unexpected stop reason: %q", generation.StopReason)
	}
	if generation.Usage.TotalTokens != 162 || generation.Usage.CacheReadInputTokens != 30 || generation.Usage.CacheCreationInputTokens != 10 {
		t.Fatalf("unexpected usage mapping: %#v", generation.Usage)
	}
	if generation.ThinkingEnabled == nil || !*generation.ThinkingEnabled {
		t.Fatalf("expected thinking enabled true, got %v", generation.ThinkingEnabled)
	}
	if len(generation.Output) != 1 || len(generation.Output[0].Parts) != 3 {
		t.Fatalf("expected text + thinking + tool call output, got %#v", generation.Output)
	}
	if generation.Output[0].Parts[0].Kind != sigil.PartKindText || generation.Output[0].Parts[0].Text != "It's 18C and sunny." {
		t.Fatalf("unexpected text output: %#v", generation.Output[0].Parts[0])
	}
	if generation.Output[0].Parts[1].Kind != sigil.PartKindThinking || generation.Output[0].Parts[1].Thinking != "answer done" {
		t.Fatalf("unexpected thinking output: %#v", generation.Output[0].Parts[1])
	}
	if generation.Output[0].Parts[2].Kind != sigil.PartKindToolCall {
		t.Fatalf("expected tool call output, got %#v", generation.Output[0].Parts[2])
	}
	if generation.Output[0].Parts[2].ToolCall.ID != "toolu_2" || generation.Output[0].Parts[2].ToolCall.Name != "weather" {
		t.Fatalf("unexpected tool call mapping: %#v", generation.Output[0].Parts[2].ToolCall)
	}
	if generation.Tags["tenant"] != "t-123" {
		t.Fatalf("expected tenant tag")
	}
	requireAnthropicArtifactKinds(t, generation.Artifacts,
		sigil.ArtifactKindRequest,
		sigil.ArtifactKindResponse,
		sigil.ArtifactKindTools,
	)
}

func TestConformance_MessageStreamNormalization(t *testing.T) {
	req := testRequest()
	summary := StreamSummary{
		Events: []asdk.BetaRawMessageStreamEventUnion{
			{
				Type: "message_start",
				Message: asdk.BetaMessage{
					ID:    "msg_delta_1",
					Model: asdk.Model("claude-sonnet-4-5"),
				},
			},
			{
				Type:  "content_block_start",
				Index: 0,
				ContentBlock: asdk.BetaRawContentBlockStartEventContentBlockUnion{
					Type: "thinking",
				},
			},
			{
				Type:  "content_block_delta",
				Index: 0,
				Delta: asdk.BetaRawMessageStreamEventUnionDelta{Thinking: "let me "},
			},
			{
				Type:  "content_block_delta",
				Index: 0,
				Delta: asdk.BetaRawMessageStreamEventUnionDelta{Thinking: "think about this"},
			},
			{
				Type:  "content_block_start",
				Index: 1,
				ContentBlock: asdk.BetaRawContentBlockStartEventContentBlockUnion{
					Type: "text",
				},
			},
			{
				Type:  "content_block_delta",
				Index: 1,
				Delta: asdk.BetaRawMessageStreamEventUnionDelta{Text: "Hello, "},
			},
			{
				Type:  "content_block_delta",
				Index: 1,
				Delta: asdk.BetaRawMessageStreamEventUnionDelta{Text: "world!"},
			},
			{
				Type:  "content_block_start",
				Index: 2,
				ContentBlock: asdk.BetaRawContentBlockStartEventContentBlockUnion{
					Type:  "tool_use",
					ID:    "toolu_1",
					Name:  "weather",
					Input: map[string]any{},
				},
			},
			{
				Type:  "content_block_delta",
				Index: 2,
				Delta: asdk.BetaRawMessageStreamEventUnionDelta{PartialJSON: `{"city"`},
			},
			{
				Type:  "content_block_delta",
				Index: 2,
				Delta: asdk.BetaRawMessageStreamEventUnionDelta{PartialJSON: `:"Berlin"}`},
			},
			{
				Type: "message_delta",
				Delta: asdk.BetaRawMessageStreamEventUnionDelta{
					StopReason: asdk.BetaStopReasonToolUse,
				},
				Usage: asdk.BetaMessageDeltaUsage{
					InputTokens:  100,
					OutputTokens: 50,
				},
			},
		},
	}

	generation, err := FromStream(req, summary,
		WithConversationID("conv-anthropic-stream"),
		WithAgentName("agent-anthropic-stream"),
		WithAgentVersion("v-anthropic-stream"),
		WithRawArtifacts(),
	)
	if err != nil {
		t.Fatalf("anthropic stream mapping: %v", err)
	}

	if generation.ConversationID != "conv-anthropic-stream" || generation.AgentName != "agent-anthropic-stream" || generation.AgentVersion != "v-anthropic-stream" {
		t.Fatalf("unexpected identity mapping: %#v", generation)
	}
	if generation.ResponseID != "msg_delta_1" || generation.ResponseModel != "claude-sonnet-4-5" {
		t.Fatalf("unexpected response mapping: id=%q model=%q", generation.ResponseID, generation.ResponseModel)
	}
	if generation.StopReason != "tool_use" {
		t.Fatalf("unexpected stop reason: %q", generation.StopReason)
	}
	if generation.Usage.TotalTokens != 150 {
		t.Fatalf("unexpected usage mapping: %#v", generation.Usage)
	}
	if len(generation.Output) != 1 || len(generation.Output[0].Parts) != 3 {
		t.Fatalf("expected thinking + text + tool call output, got %#v", generation.Output)
	}
	if generation.Output[0].Parts[0].Kind != sigil.PartKindThinking || generation.Output[0].Parts[0].Thinking != "let me think about this" {
		t.Fatalf("unexpected thinking output: %#v", generation.Output[0].Parts[0])
	}
	if generation.Output[0].Parts[1].Kind != sigil.PartKindText || generation.Output[0].Parts[1].Text != "Hello, world!" {
		t.Fatalf("unexpected text output: %#v", generation.Output[0].Parts[1])
	}
	if generation.Output[0].Parts[2].Kind != sigil.PartKindToolCall {
		t.Fatalf("expected tool call output, got %#v", generation.Output[0].Parts[2])
	}
	if string(generation.Output[0].Parts[2].ToolCall.InputJSON) != `{"city":"Berlin"}` {
		t.Fatalf("unexpected streamed tool input: %q", string(generation.Output[0].Parts[2].ToolCall.InputJSON))
	}
	requireAnthropicArtifactKinds(t, generation.Artifacts,
		sigil.ArtifactKindRequest,
		sigil.ArtifactKindTools,
		sigil.ArtifactKindProviderEvent,
	)
}

func TestConformance_AnthropicErrorMapping(t *testing.T) {
	if _, err := FromRequestResponse(testRequest(), nil); err == nil || err.Error() != "response is required" {
		t.Fatalf("expected explicit response error, got %v", err)
	}
	if _, err := FromStream(testRequest(), StreamSummary{}); err == nil || err.Error() != "stream summary has no events and no final message" {
		t.Fatalf("expected explicit stream error, got %v", err)
	}

	_, err := FromRequestResponse(
		testRequest(),
		&asdk.BetaMessage{Model: asdk.Model("claude-sonnet-4-5")},
		WithProviderName(""),
	)
	if err == nil || err.Error() != "generation.model.provider is required" {
		t.Fatalf("expected explicit validation error for invalid provider mapping, got %v", err)
	}
}

func requireAnthropicArtifactKinds(t *testing.T, artifacts []sigil.Artifact, want ...sigil.ArtifactKind) {
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
