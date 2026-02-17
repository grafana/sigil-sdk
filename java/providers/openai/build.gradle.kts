plugins {
    `java-library`
}

dependencies {
    api(project(":core"))
    compileOnly(libs.openai.java)
    testImplementation(libs.openai.java)

    testImplementation(platform(libs.junit.bom))
    testImplementation(libs.junit.jupiter)
    testRuntimeOnly("org.junit.platform:junit-platform-launcher")
    testImplementation(libs.assertj.core)
}
