package googleadk

import "errors"

// ErrEmbeddingsUnsupported reports that the Google ADK lifecycle surface used
// by this helper does not currently expose a dedicated embeddings callback.
var ErrEmbeddingsUnsupported = errors.New("googleadk: embeddings are not supported because the Google ADK lifecycle surface does not expose a dedicated embeddings callback")

// CheckEmbeddingsSupport reports whether this helper can wrap a native Google
// ADK embeddings lifecycle. The current lifecycle surface only exposes run and
// tool callbacks, so callers should treat a non-nil error as an explicit
// capability gate instead of assuming embedding conformance coverage exists.
func CheckEmbeddingsSupport() error {
	return ErrEmbeddingsUnsupported
}
