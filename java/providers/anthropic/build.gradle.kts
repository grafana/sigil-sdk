plugins {
    `java-library`
}

dependencies {
    api(project(":core"))
    implementation(project(":providers:openai"))
    compileOnly(libs.anthropic.java)

    testImplementation(platform(libs.junit.bom))
    testImplementation(libs.junit.jupiter)
    testImplementation(libs.assertj.core)
}
