plugins {
    alias(libs.plugins.jmh) apply false
    alias(libs.plugins.protobuf) apply false
}

allprojects {
    group = "com.grafana.sigil"
    version = "0.1.0"
}

subprojects {
    plugins.withId("java") {
        extensions.configure<JavaPluginExtension> {
            toolchain {
                languageVersion.set(JavaLanguageVersion.of(17))
            }
        }

        tasks.withType<Test>().configureEach {
            useJUnitPlatform()
        }
    }
}
