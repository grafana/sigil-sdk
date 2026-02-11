package anthropic

import (
	"context"

	asdk "github.com/anthropics/anthropic-sdk-go"

	"github.com/grafana/sigil/sdks/go/sigil"
)

// Message calls the Anthropic messages API and records the generation.
// It mirrors providerClient.Beta.Messages.New but adds Sigil recording.
// The native *asdk.BetaMessage response is returned unchanged.
func Message(
	ctx context.Context,
	client *sigil.Client,
	provider asdk.Client,
	req asdk.BetaMessageNewParams,
	opts ...Option,
) (*asdk.BetaMessage, error) {
	options := applyOptions(opts)

	ctx, rec := client.StartGeneration(ctx, sigil.GenerationStart{
		ConversationID: options.conversationID,
		Model:          sigil.ModelRef{Provider: options.providerName, Name: string(req.Model)},
	})
	defer rec.End()

	resp, err := provider.Beta.Messages.New(ctx, req)
	if err != nil {
		rec.SetCallError(err)
		return nil, err
	}

	rec.SetResult(FromRequestResponse(req, resp, opts...))
	return resp, rec.Err()
}

// MessageStream calls the Anthropic streaming messages API and records the generation.
// It mirrors providerClient.Beta.Messages.NewStreaming but adds Sigil recording.
// All events are collected into StreamSummary; for per-event processing use the
// defer pattern directly with StartGeneration.
func MessageStream(
	ctx context.Context,
	client *sigil.Client,
	provider asdk.Client,
	req asdk.BetaMessageNewParams,
	opts ...Option,
) (*asdk.BetaMessage, StreamSummary, error) {
	options := applyOptions(opts)

	ctx, rec := client.StartGeneration(ctx, sigil.GenerationStart{
		ConversationID: options.conversationID,
		Model:          sigil.ModelRef{Provider: options.providerName, Name: string(req.Model)},
	})
	defer rec.End()

	stream := provider.Beta.Messages.NewStreaming(ctx, req)
	defer stream.Close()

	summary := StreamSummary{}
	for stream.Next() {
		summary.Events = append(summary.Events, stream.Current())
	}
	if err := stream.Err(); err != nil {
		rec.SetCallError(err)
		return nil, summary, err
	}

	rec.SetResult(FromStream(req, summary, opts...))

	if summary.FinalMessage != nil {
		return summary.FinalMessage, summary, rec.Err()
	}
	return nil, summary, rec.Err()
}
