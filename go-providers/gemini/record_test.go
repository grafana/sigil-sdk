package gemini

import (
	"context"
	"errors"
	"strings"
	"testing"

	"google.golang.org/genai"

	"github.com/grafana/sigil/sdks/go/sigil"
)

func TestEmbedContentReturnsRecorderValidationErrorAfterEnd(t *testing.T) {
	client := newEmbeddingTestClient(t)

	contents := []*genai.Content{
		genai.NewContentFromText("hello", genai.RoleUser),
	}
	expectedResponse := &genai.EmbedContentResponse{
		Embeddings: []*genai.ContentEmbedding{
			{
				Values: []float32{0.1, 0.2},
				Statistics: &genai.ContentEmbeddingStatistics{
					TokenCount: 2,
				},
			},
		},
	}

	response, err := embedContent(
		context.Background(),
		client,
		"gemini-embedding-001",
		contents,
		nil,
		func(context.Context, string, []*genai.Content, *genai.EmbedContentConfig) (*genai.EmbedContentResponse, error) {
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

func TestEmbedContentPreservesProviderErrors(t *testing.T) {
	client := newEmbeddingTestClient(t)

	providerErr := errors.New("provider failed")

	response, err := embedContent(
		context.Background(),
		client,
		"gemini-embedding-001",
		nil,
		nil,
		func(context.Context, string, []*genai.Content, *genai.EmbedContentConfig) (*genai.EmbedContentResponse, error) {
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
