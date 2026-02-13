plugins {
    java
    alias(libs.plugins.jmh)
}

dependencies {
    implementation(project(":core"))
    implementation(project(":providers:openai"))
    implementation(project(":providers:anthropic"))
    implementation(project(":providers:gemini"))
}

jmh {
    warmupIterations.set(2)
    iterations.set(5)
    fork.set(1)
    resultFormat.set("JSON")
    resultsFile.set(layout.buildDirectory.file("reports/jmh/results.json"))
}
