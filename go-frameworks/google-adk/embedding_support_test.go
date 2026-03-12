package googleadk

import (
	"errors"
	"testing"
)

func TestCheckEmbeddingsSupportReturnsUnsupportedError(t *testing.T) {
	err := CheckEmbeddingsSupport()
	if err == nil {
		t.Fatalf("expected embeddings support error")
	}
	if !errors.Is(err, ErrEmbeddingsUnsupported) {
		t.Fatalf("expected ErrEmbeddingsUnsupported, got %v", err)
	}
	if got, want := err.Error(), ErrEmbeddingsUnsupported.Error(); got != want {
		t.Fatalf("unexpected embeddings support error: got %q want %q", got, want)
	}
}
