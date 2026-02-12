package gemini

import (
	"context"
	"os"

	"github.com/grafana/sigil/sdks/go/sigil"
	"google.golang.org/genai"
)

// Example_withSigilWrapper shows the one-liner wrapper approach.
func Example_withSigilWrapper() {
	if os.Getenv("SIGIL_RUN_LIVE_EXAMPLES") != "1" {
		return
	}

	apiKey := geminiAPIKey()
	if apiKey == "" {
		return
	}

	client := sigil.NewClient(sigil.DefaultConfig())
	req := exampleGeminiRequest()

	providerClient, err := genai.NewClient(context.Background(), &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		panic(err)
	}

	resp, err := GenerateContent(context.Background(), client, providerClient, req,
		WithConversationID("conv-gemini-1"),
	)
	if err != nil {
		panic(err)
	}

	_ = resp.Candidates[0].Content.Parts[0].Text
}

// Example_withSigilDefer shows the defer pattern for full control.
func Example_withSigilDefer() {
	if os.Getenv("SIGIL_RUN_LIVE_EXAMPLES") != "1" {
		return
	}

	apiKey := geminiAPIKey()
	if apiKey == "" {
		return
	}

	client := sigil.NewClient(sigil.DefaultConfig())
	req := exampleGeminiRequest()

	ctx, rec := client.StartGeneration(context.Background(), sigil.GenerationStart{
		ConversationID: "conv-gemini-2",
		Model:          sigil.ModelRef{Provider: "gemini", Name: req.Model},
	})
	defer rec.End()

	providerClient, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		rec.SetCallError(err)
		return
	}

	resp, err := providerClient.Models.GenerateContent(ctx, req.Model, req.Contents, req.Config)
	if err != nil {
		rec.SetCallError(err)
		return
	}

	rec.SetResult(FromRequestResponse(req, resp, WithConversationID("conv-gemini-2")))
	_ = resp.Candidates[0].Content.Parts[0].Text
}

// Example_withSigilStreamingDefer shows the defer pattern for streaming with per-response processing.
func Example_withSigilStreamingDefer() {
	if os.Getenv("SIGIL_RUN_LIVE_EXAMPLES") != "1" {
		return
	}

	apiKey := geminiAPIKey()
	if apiKey == "" {
		return
	}

	client := sigil.NewClient(sigil.DefaultConfig())
	req := exampleGeminiRequest()

	ctx, rec := client.StartStreamingGeneration(context.Background(), sigil.GenerationStart{
		ConversationID: "conv-gemini-3",
		Model:          sigil.ModelRef{Provider: "gemini", Name: req.Model},
	})
	defer rec.End()

	providerClient, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		rec.SetCallError(err)
		return
	}

	summary := StreamSummary{}
	for response, err := range providerClient.Models.GenerateContentStream(ctx, req.Model, req.Contents, req.Config) {
		if err != nil {
			rec.SetCallError(err)
			return
		}
		if response != nil {
			summary.Responses = append(summary.Responses, response)
			// Process each response here (e.g., SSE forwarding).
		}
	}

	rec.SetResult(FromStream(req, summary, WithConversationID("conv-gemini-3")))
}

func exampleGeminiRequest() GenerateContentRequest {
	return GenerateContentRequest{
		Model: "gemini-2.5-pro",
		Contents: []*genai.Content{
			genai.NewContentFromText("Hello", genai.RoleUser),
		},
	}
}

func geminiAPIKey() string {
	if key := os.Getenv("GOOGLE_API_KEY"); key != "" {
		return key
	}
	return os.Getenv("GEMINI_API_KEY")
}
