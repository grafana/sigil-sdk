package sigil_test

import (
	"context"
	"strings"

	"github.com/grafana/sigil/sdks/go/sigil"
)

func ExampleClient_StartGeneration() {
	client := sigil.NewClient(sigil.DefaultConfig())

	callCtx, recorder, err := client.StartGeneration(context.Background(), sigil.GenerationStart{
		ThreadID: "thread-9b2f",
		Model:    sigil.ModelRef{Provider: "anthropic", Name: "claude-sonnet-4-5"},
	})
	if err != nil {
		panic(err)
	}

	// Use callCtx for the provider request so the request is inside the generation span.
	_ = callCtx

	// Keep the provider response in normal local scope.
	responseText := "Hi!"

	generation := sigil.Generation{
		Input: []sigil.Message{
			{Role: sigil.RoleUser, Parts: []sigil.Part{sigil.TextPart("Hello")}},
		},
		Output: []sigil.Message{
			{Role: sigil.RoleAssistant, Parts: []sigil.Part{sigil.TextPart(responseText)}},
		},
		Usage: sigil.TokenUsage{InputTokens: 120, OutputTokens: 42},
	}

	if err := recorder.End(generation, nil); err != nil {
		panic(err)
	}
}

func ExampleClient_StartStreamingGeneration() {
	client := sigil.NewClient(sigil.DefaultConfig())

	callCtx, recorder, err := client.StartStreamingGeneration(context.Background(), sigil.GenerationStart{
		ThreadID: "thread-stream",
		Model:    sigil.ModelRef{Provider: "openai", Name: "gpt-5"},
	})
	if err != nil {
		panic(err)
	}

	_ = callCtx

	chunks := []string{"Hel", "lo", " ", "world"}
	assistantText := strings.Join(chunks, "")

	generation := sigil.Generation{
		Input: []sigil.Message{
			{Role: sigil.RoleUser, Parts: []sigil.Part{sigil.TextPart("Say hello")}},
		},
		Output: []sigil.Message{
			{Role: sigil.RoleAssistant, Parts: []sigil.Part{sigil.TextPart(assistantText)}},
		},
	}

	if err := recorder.End(generation, nil); err != nil {
		panic(err)
	}
}
