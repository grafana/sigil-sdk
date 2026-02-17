package sigil

import "errors"

// Sentinel errors for errors.Is matching.
var (
	// ErrNilClient is returned when a nil *Client is used.
	ErrNilClient = errors.New("sigil: nil client")
	// ErrNilRecorder is returned when a nil recorder is used.
	ErrNilRecorder = errors.New("sigil: nil recorder")
	// ErrRecorderAlreadyEnded is returned on duplicate End calls.
	ErrRecorderAlreadyEnded = errors.New("sigil: recorder already ended")
	// ErrRecorderNotReady is returned when a recorder has nil internals.
	ErrRecorderNotReady = errors.New("sigil: recorder not initialized")
	// ErrToolNameRequired is returned when StartToolExecution receives an empty tool name.
	ErrToolNameRequired = errors.New("sigil: tool name is required")
	// ErrValidationFailed wraps generation validation failures.
	ErrValidationFailed = errors.New("sigil: generation validation failed")
	// ErrEmbeddingValidationFailed wraps embedding validation failures.
	ErrEmbeddingValidationFailed = errors.New("sigil: embedding validation failed")
	// ErrEnqueueFailed wraps generation enqueue failures.
	ErrEnqueueFailed = errors.New("sigil: generation enqueue failed")
	// ErrQueueFull is returned when the generation queue is at capacity.
	ErrQueueFull = errors.New("sigil: generation queue is full")
	// ErrClientShutdown is returned when enqueue happens after shutdown starts.
	ErrClientShutdown = errors.New("sigil: client is shutting down")
	// ErrMappingFailed wraps provider-to-generation mapping failures.
	ErrMappingFailed = errors.New("sigil: generation mapping failed")
	// ErrRatingValidationFailed wraps conversation rating input validation failures.
	ErrRatingValidationFailed = errors.New("sigil: conversation rating validation failed")
	// ErrRatingConflict wraps idempotency conflicts when submitting conversation ratings.
	ErrRatingConflict = errors.New("sigil: conversation rating conflict")
	// ErrRatingTransportFailed wraps conversation rating transport failures.
	ErrRatingTransportFailed = errors.New("sigil: conversation rating transport failed")
)
