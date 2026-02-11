package openai

import (
	"context"
	"os"

	"github.com/grafana/sigil/sdks/go/sigil"
	osdk "github.com/openai/openai-go"
	osdkoption "github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

// Example_withSigilWrapper shows the one-liner wrapper approach.
func Example_withSigilWrapper() {
	if os.Getenv("SIGIL_RUN_LIVE_EXAMPLES") != "1" {
		return
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return
	}

	client := sigil.NewClient(sigil.DefaultConfig())
	providerClient := osdk.NewClient(osdkoption.WithAPIKey(apiKey))
	req := exampleOpenAIRequest()

	resp, err := ChatCompletion(context.Background(), client, providerClient, req,
		WithConversationID("conv-openai-1"),
	)
	if err != nil {
		panic(err)
	}

	_ = resp.Choices[0].Message.Content
}

// Example_withSigilDefer shows the defer pattern for full control.
func Example_withSigilDefer() {
	if os.Getenv("SIGIL_RUN_LIVE_EXAMPLES") != "1" {
		return
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return
	}

	client := sigil.NewClient(sigil.DefaultConfig())
	providerClient := osdk.NewClient(osdkoption.WithAPIKey(apiKey))
	req := exampleOpenAIRequest()

	ctx, rec := client.StartGeneration(context.Background(), sigil.GenerationStart{
		ConversationID: "conv-openai-2",
		Model:          sigil.ModelRef{Provider: "openai", Name: string(req.Model)},
	})
	defer rec.End()

	resp, err := providerClient.Chat.Completions.New(ctx, req)
	if err != nil {
		rec.SetCallError(err)
		return
	}

	rec.SetResult(FromRequestResponse(req, resp, WithConversationID("conv-openai-2")))
	_ = resp.Choices[0].Message.Content
}

// Example_withSigilStreamingWrapper shows the streaming wrapper approach.
func Example_withSigilStreamingWrapper() {
	if os.Getenv("SIGIL_RUN_LIVE_EXAMPLES") != "1" {
		return
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return
	}

	client := sigil.NewClient(sigil.DefaultConfig())
	providerClient := osdk.NewClient(osdkoption.WithAPIKey(apiKey))
	req := exampleOpenAIRequest()

	_, _, err := ChatCompletionStream(context.Background(), client, providerClient, req,
		WithConversationID("conv-openai-3"),
	)
	if err != nil {
		panic(err)
	}
}

// Example_withSigilStreamingDefer shows the defer pattern for streaming with per-chunk processing.
func Example_withSigilStreamingDefer() {
	if os.Getenv("SIGIL_RUN_LIVE_EXAMPLES") != "1" {
		return
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return
	}

	client := sigil.NewClient(sigil.DefaultConfig())
	providerClient := osdk.NewClient(osdkoption.WithAPIKey(apiKey))
	req := exampleOpenAIRequest()

	ctx, rec := client.StartGeneration(context.Background(), sigil.GenerationStart{
		ConversationID: "conv-openai-4",
		Model:          sigil.ModelRef{Provider: "openai", Name: string(req.Model)},
	})
	defer rec.End()

	stream := providerClient.Chat.Completions.NewStreaming(ctx, req)
	defer stream.Close()

	summary := StreamSummary{}
	for stream.Next() {
		chunk := stream.Current()
		summary.Chunks = append(summary.Chunks, chunk)
		// Process each chunk here (e.g., SSE forwarding).
	}
	if err := stream.Err(); err != nil {
		rec.SetCallError(err)
		return
	}

	rec.SetResult(FromStream(req, summary, WithConversationID("conv-openai-4")))
}

func exampleOpenAIRequest() osdk.ChatCompletionNewParams {
	return osdk.ChatCompletionNewParams{
		Model: shared.ChatModel("gpt-4o-mini"),
		Messages: []osdk.ChatCompletionMessageParamUnion{
			osdk.UserMessage("Hello"),
		},
	}
}
