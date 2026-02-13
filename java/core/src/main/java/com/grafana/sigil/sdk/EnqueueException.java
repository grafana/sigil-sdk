package com.grafana.sigil.sdk;

/** Raised when generation enqueue fails locally. */
public final class EnqueueException extends SigilException {
    public EnqueueException(String message, Throwable cause) {
        super(message, cause);
    }

    public EnqueueException(String message) {
        super(message);
    }
}
