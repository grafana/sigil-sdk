plugins {
    alias(libs.plugins.jmh) apply false
    alias(libs.plugins.protobuf) apply false
    alias(libs.plugins.maven.publish) apply false
}

allprojects {
    group = "com.grafana.sigil"
    version = findProperty("version")?.toString() ?: "0.1.0-SNAPSHOT"
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
            // Clear canonical SIGIL_* env vars so individual tests don't pick
            // up developer-machine config when they construct a SigilClient.
            // Tests that exercise env layering should pass an explicit lookup
            // to SigilEnvConfig.resolveFromEnv.
            environment("SIGIL_ENDPOINT", "")
            environment("SIGIL_PROTOCOL", "")
            environment("SIGIL_INSECURE", "")
            environment("SIGIL_HEADERS", "")
            environment("SIGIL_AUTH_MODE", "")
            environment("SIGIL_AUTH_TENANT_ID", "")
            environment("SIGIL_AUTH_TOKEN", "")
            environment("SIGIL_AGENT_NAME", "")
            environment("SIGIL_AGENT_VERSION", "")
            environment("SIGIL_USER_ID", "")
            environment("SIGIL_TAGS", "")
            environment("SIGIL_CONTENT_CAPTURE_MODE", "")
            environment("SIGIL_DEBUG", "")
        }
    }

    plugins.withId("com.vanniktech.maven.publish") {
        extensions.configure<com.vanniktech.maven.publish.MavenPublishBaseExtension> {
            publishToMavenCentral(com.vanniktech.maven.publish.SonatypeHost.CENTRAL_PORTAL)
            signAllPublications()

            pom {
                name.set(project.name)
                description.set("Sigil SDK for Java - ${project.name}")
                url.set("https://github.com/grafana/sigil-sdk")
                inceptionYear.set("2025")

                licenses {
                    license {
                        name.set("Apache License, Version 2.0")
                        url.set("https://www.apache.org/licenses/LICENSE-2.0.txt")
                        distribution.set("repo")
                    }
                }

                developers {
                    developer {
                        id.set("grafana")
                        name.set("Grafana Labs")
                        url.set("https://grafana.com")
                    }
                }

                scm {
                    url.set("https://github.com/grafana/sigil-sdk")
                    connection.set("scm:git:git://github.com/grafana/sigil-sdk.git")
                    developerConnection.set("scm:git:ssh://git@github.com/grafana/sigil-sdk.git")
                }
            }
        }
    }
}
