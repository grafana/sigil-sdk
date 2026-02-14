package com.grafana.sigil.sdk;

/** Raised when conversation rating submission transport fails. */
public final class RatingTransportException extends SigilException {
    public RatingTransportException(String message) {
        super(message);
    }

    public RatingTransportException(String message, Throwable cause) {
        super(message, cause);
    }
}
