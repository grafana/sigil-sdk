package com.grafana.sigil.sdk;

/** Raised when a rating idempotency key conflicts with a different payload. */
public final class RatingConflictException extends SigilException {
    public RatingConflictException(String message) {
        super(message);
    }

    public RatingConflictException(String message, Throwable cause) {
        super(message, cause);
    }
}
