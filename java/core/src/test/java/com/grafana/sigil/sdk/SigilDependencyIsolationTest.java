package com.grafana.sigil.sdk;

import static org.assertj.core.api.Assertions.assertThatThrownBy;

import org.junit.jupiter.api.Test;

class SigilDependencyIsolationTest {
    @Test
    void coreModuleDoesNotDependOnProviderAdapters() {
        assertThatThrownBy(() -> Class.forName("com.grafana.sigil.sdk.providers.openai.OpenAiAdapter"))
                .isInstanceOf(ClassNotFoundException.class);
    }
}
