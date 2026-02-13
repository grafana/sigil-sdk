package com.grafana.sigil.sdk;

/** Raised when generation queue is full. */
public final class QueueFullException extends SigilException {
    public QueueFullException(String message) {
        super(message);
    }
}
