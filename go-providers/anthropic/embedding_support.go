package anthropic

import "errors"

// ErrEmbeddingsUnsupported reports that the official Anthropic SDK/API surface
// used by this helper does not expose a native embeddings endpoint.
var ErrEmbeddingsUnsupported = errors.New("anthropic: embeddings are not supported by the official Anthropic SDK/API surface")

// CheckEmbeddingsSupport reports whether this helper can wrap a native Anthropic
// embeddings API. The current official Anthropic SDK/API surface used by this
// module does not expose embeddings, so callers should treat a non-nil error as
// a hard capability boundary rather than inventing custom request DTOs.
func CheckEmbeddingsSupport() error {
	return ErrEmbeddingsUnsupported
}
