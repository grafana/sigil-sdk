package openai

import (
	"context"
	"errors"
	"strings"
	"testing"

	osdk "github.com/openai/openai-go/v3"

	"github.com/grafana/sigil/sdks/go/sigil"
)

func TestEmbeddingsNewReturnsRecorderValidationErrorAfterEnd(t *testing.T) {
	client := newEmbeddingTestClient(t)

	req := osdk.EmbeddingNewParams{
		Model: osdk.EmbeddingModel("text-embedding-3-small"),
	}
	expectedResponse := &osdk.CreateEmbeddingResponse{
		Model: "text-embedding-3-small",
		Data: []osdk.Embedding{
			{Embedding: []float64{0.1, 0.2}},
		},
		Usage: osdk.CreateEmbeddingResponseUsage{
			PromptTokens: 2,
			TotalTokens:  2,
		},
	}

	response, err := embeddingsNew(
		context.Background(),
		client,
		req,
		func(context.Context, osdk.EmbeddingNewParams) (*osdk.CreateEmbeddingResponse, error) {
			return expectedResponse, nil
		},
		WithProviderName(""),
	)
	if err == nil {
		t.Fatalf("expected recorder validation error")
	}
	if !strings.Contains(err.Error(), "embedding.model.provider is required") {
		t.Fatalf("expected embedding model provider validation error, got %v", err)
	}
	if response != expectedResponse {
		t.Fatalf("expected wrapper to return provider response pointer")
	}
}

func TestEmbeddingsNewPreservesProviderErrors(t *testing.T) {
	client := newEmbeddingTestClient(t)

	req := osdk.EmbeddingNewParams{
		Model: osdk.EmbeddingModel("text-embedding-3-small"),
	}
	providerErr := errors.New("provider failed")

	response, err := embeddingsNew(
		context.Background(),
		client,
		req,
		func(context.Context, osdk.EmbeddingNewParams) (*osdk.CreateEmbeddingResponse, error) {
			return nil, providerErr
		},
		WithProviderName(""),
	)
	if !errors.Is(err, providerErr) {
		t.Fatalf("expected provider error, got %v", err)
	}
	if response != nil {
		t.Fatalf("expected nil response on provider error")
	}
}

func newEmbeddingTestClient(t *testing.T) *sigil.Client {
	t.Helper()

	cfg := sigil.DefaultConfig()
	cfg.GenerationExport.Protocol = sigil.GenerationExportProtocolNone

	client := sigil.NewClient(cfg)
	t.Cleanup(func() {
		if err := client.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown sigil client: %v", err)
		}
	})
	return client
}
