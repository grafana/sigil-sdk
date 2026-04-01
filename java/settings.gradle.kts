rootProject.name = "sigil-sdk-java"

include(":core")
include(":providers:openai")
include(":providers:anthropic")
include(":providers:gemini")
include(":frameworks:google-adk")
include(":benchmarks")

project(":providers:openai").projectDir = file("providers/openai")
project(":providers:anthropic").projectDir = file("providers/anthropic")
project(":providers:gemini").projectDir = file("providers/gemini")
project(":frameworks:google-adk").projectDir = file("frameworks/google-adk")

pluginManagement {
    repositories {
        gradlePluginPortal()
        mavenCentral()
    }
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        mavenCentral()
    }
}
