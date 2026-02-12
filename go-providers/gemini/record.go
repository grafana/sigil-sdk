package gemini

import (
	"context"

	"google.golang.org/genai"

	"github.com/grafana/sigil/sdks/go/sigil"
)

// GenerateContent calls the Gemini generate-content API and records the generation.
// It mirrors providerClient.Models.GenerateContent but adds Sigil recording.
// The native *genai.GenerateContentResponse is returned unchanged.
func GenerateContent(
	ctx context.Context,
	client *sigil.Client,
	provider *genai.Client,
	req GenerateContentRequest,
	opts ...Option,
) (*genai.GenerateContentResponse, error) {
	options := applyOptions(opts)

	ctx, rec := client.StartGeneration(ctx, sigil.GenerationStart{
		ConversationID: options.conversationID,
		AgentName:      options.agentName,
		AgentVersion:   options.agentVersion,
		Model:          sigil.ModelRef{Provider: options.providerName, Name: req.Model},
	})
	defer rec.End()

	resp, err := provider.Models.GenerateContent(ctx, req.Model, req.Contents, req.Config)
	if err != nil {
		rec.SetCallError(err)
		return nil, err
	}

	rec.SetResult(FromRequestResponse(req, resp, opts...))
	return resp, rec.Err()
}

// GenerateContentStream calls the Gemini streaming generate-content API and records the generation.
// It mirrors providerClient.Models.GenerateContentStream but adds Sigil recording.
// All responses are collected into StreamSummary; for per-response processing use the
// defer pattern directly with StartStreamingGeneration.
func GenerateContentStream(
	ctx context.Context,
	client *sigil.Client,
	provider *genai.Client,
	req GenerateContentRequest,
	opts ...Option,
) (StreamSummary, error) {
	options := applyOptions(opts)

	ctx, rec := client.StartStreamingGeneration(ctx, sigil.GenerationStart{
		ConversationID: options.conversationID,
		AgentName:      options.agentName,
		AgentVersion:   options.agentVersion,
		Model:          sigil.ModelRef{Provider: options.providerName, Name: req.Model},
	})
	defer rec.End()

	summary := StreamSummary{}
	for response, err := range provider.Models.GenerateContentStream(ctx, req.Model, req.Contents, req.Config) {
		if err != nil {
			rec.SetCallError(err)
			return summary, err
		}
		if response != nil {
			summary.Responses = append(summary.Responses, response)
		}
	}

	rec.SetResult(FromStream(req, summary, opts...))
	return summary, rec.Err()
}
