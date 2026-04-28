plugins {
    `java-library`
    alias(libs.plugins.maven.publish)
}

mavenPublishing {
    coordinates(artifactId = "sigil-sdk-anthropic")
}

dependencies {
    api(project(":core"))
    compileOnly(libs.anthropic.java)

    testImplementation(libs.anthropic.java)
    testImplementation(platform(libs.junit.bom))
    testImplementation(libs.junit.jupiter)
    testRuntimeOnly("org.junit.platform:junit-platform-launcher")
    testImplementation(libs.assertj.core)
}
