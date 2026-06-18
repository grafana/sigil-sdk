package com.grafana.sigil.sdk;

/** Mutates or replaces a normalized generation before export. */
@FunctionalInterface
public interface GenerationSanitizer {
    /**
     * Sanitizes a normalized generation payload.
     *
     * <p>Implementations may mutate string and byte payloads, but should
     * preserve message and part structure.</p>
     */
    Generation sanitize(Generation generation);
}
