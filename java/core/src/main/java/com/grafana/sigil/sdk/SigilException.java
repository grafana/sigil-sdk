package com.grafana.sigil.sdk;

/** Base SDK runtime exception. */
public class SigilException extends RuntimeException {
    public SigilException(String message) {
        super(message);
    }

    public SigilException(String message, Throwable cause) {
        super(message, cause);
    }
}
