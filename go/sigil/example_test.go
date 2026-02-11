package sigil_test

import (
	"context"
	"strings"

	"github.com/grafana/sigil/sdks/go/sigil"
)

func ExampleClient_StartGeneration() {
	client := sigil.NewClient(sigil.DefaultConfig())

	ctx, recorder := client.StartGeneration(context.Background(), sigil.GenerationStart{
		ConversationID: "conv-9b2f",
		Model:          sigil.ModelRef{Provider: "anthropic", Name: "claude-sonnet-4-5"},
	})
	defer recorder.End()

	// Use ctx for the provider request so the request is inside the generation span.
	_ = ctx

	// Keep the provider response in normal local scope.
	responseText := "Hi!"

	recorder.SetResult(sigil.Generation{
		Input:  []sigil.Message{sigil.UserTextMessage("Hello")},
		Output: []sigil.Message{sigil.AssistantTextMessage(responseText)},
		Usage:  sigil.TokenUsage{InputTokens: 120, OutputTokens: 42},
	}, nil)
}

func ExampleClient_StartGeneration_streaming() {
	client := sigil.NewClient(sigil.DefaultConfig())

	ctx, recorder := client.StartGeneration(context.Background(), sigil.GenerationStart{
		ConversationID: "conv-stream",
		Model:          sigil.ModelRef{Provider: "openai", Name: "gpt-5"},
	})
	defer recorder.End()

	_ = ctx

	chunks := []string{"Hel", "lo", " ", "world"}
	assistantText := strings.Join(chunks, "")

	recorder.SetResult(sigil.Generation{
		Input:  []sigil.Message{sigil.UserTextMessage("Say hello")},
		Output: []sigil.Message{sigil.AssistantTextMessage(assistantText)},
	}, nil)
}

func ExampleClient_StartToolExecution() {
	client := sigil.NewClient(sigil.DefaultConfig())

	ctx, recorder := client.StartToolExecution(context.Background(), sigil.ToolExecutionStart{
		ToolName:        "weather",
		ToolCallID:      "call_weather",
		ToolType:        "function",
		ToolDescription: "Get weather for a city",
		ConversationID:  "conv-tools",
		IncludeContent:  true,
	})
	defer recorder.End()

	_ = ctx
	result := map[string]any{"temp_c": 18}

	recorder.SetResult(sigil.ToolExecutionEnd{
		Arguments: map[string]any{"city": "Paris"},
		Result:    result,
	})
}
