package openai

import (
	"context"

	osdk "github.com/openai/openai-go"

	"github.com/grafana/sigil/sdks/go/sigil"
)

// ChatCompletion calls the OpenAI chat completion API and records the generation.
// It mirrors providerClient.Chat.Completions.New but adds Sigil recording.
// The native *osdk.ChatCompletion response is returned unchanged.
func ChatCompletion(
	ctx context.Context,
	client *sigil.Client,
	provider osdk.Client,
	req osdk.ChatCompletionNewParams,
	opts ...Option,
) (*osdk.ChatCompletion, error) {
	options := applyOptions(opts)

	ctx, rec := client.StartGeneration(ctx, sigil.GenerationStart{
		ConversationID: options.conversationID,
		Model:          sigil.ModelRef{Provider: options.providerName, Name: string(req.Model)},
	})
	defer rec.End()

	resp, err := provider.Chat.Completions.New(ctx, req)
	if err != nil {
		rec.SetCallError(err)
		return nil, err
	}

	rec.SetResult(FromRequestResponse(req, resp, opts...))
	return resp, rec.Err()
}

// ChatCompletionStream calls the OpenAI streaming chat completion API and records the generation.
// It mirrors providerClient.Chat.Completions.NewStreaming but adds Sigil recording.
// All chunks are collected into StreamSummary; for per-chunk processing use the
// defer pattern directly with StartGeneration.
func ChatCompletionStream(
	ctx context.Context,
	client *sigil.Client,
	provider osdk.Client,
	req osdk.ChatCompletionNewParams,
	opts ...Option,
) (*osdk.ChatCompletion, StreamSummary, error) {
	options := applyOptions(opts)

	ctx, rec := client.StartGeneration(ctx, sigil.GenerationStart{
		ConversationID: options.conversationID,
		Model:          sigil.ModelRef{Provider: options.providerName, Name: string(req.Model)},
	})
	defer rec.End()

	stream := provider.Chat.Completions.NewStreaming(ctx, req)
	defer stream.Close()

	summary := StreamSummary{}
	for stream.Next() {
		summary.Chunks = append(summary.Chunks, stream.Current())
	}
	if err := stream.Err(); err != nil {
		rec.SetCallError(err)
		return nil, summary, err
	}

	rec.SetResult(FromStream(req, summary, opts...))

	// If there's a final response with choices, return it.
	if summary.FinalResponse != nil {
		return summary.FinalResponse, summary, rec.Err()
	}
	return nil, summary, rec.Err()
}
