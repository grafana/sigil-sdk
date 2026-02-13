plugins {
    application
}

dependencies {
    implementation(project(":core"))
    implementation(project(":providers:openai"))
    implementation(project(":providers:anthropic"))
    implementation(project(":providers:gemini"))
    implementation(libs.openai.java)

    testImplementation(platform(libs.junit.bom))
    testImplementation(libs.junit.jupiter)
    testImplementation(libs.assertj.core)
}

application {
    mainClass.set("com.grafana.sigil.sdk.devex.DevexEmitter")
}
