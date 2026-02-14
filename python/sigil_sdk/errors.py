"""Error hierarchy used by Sigil Python SDK."""


class SigilError(Exception):
    """Base class for SDK-specific errors."""


class ValidationError(SigilError):
    """Raised when generation validation fails before enqueue."""


class EnqueueError(SigilError):
    """Raised when generation enqueue fails."""


class QueueFullError(EnqueueError):
    """Raised when generation queue is full."""


class ClientShutdownError(EnqueueError):
    """Raised when enqueue happens while shutdown is in progress."""


class MappingError(SigilError):
    """Raised when provider mapper logic fails."""


class RatingConflictError(SigilError):
    """Raised when rating idempotency key conflicts with a different payload."""


class RatingTransportError(SigilError):
    """Raised when rating submission transport fails."""
