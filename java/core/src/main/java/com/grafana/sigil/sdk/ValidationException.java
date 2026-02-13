package com.grafana.sigil.sdk;

/** Raised when generation payload validation fails. */
public final class ValidationException extends SigilException {
    public ValidationException(String message) {
        super(message);
    }
}
