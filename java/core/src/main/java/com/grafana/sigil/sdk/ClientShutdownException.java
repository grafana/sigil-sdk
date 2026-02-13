package com.grafana.sigil.sdk;

/** Raised when runtime APIs are called after shutdown. */
public final class ClientShutdownException extends SigilException {
    public ClientShutdownException(String message) {
        super(message);
    }
}
