rootProject.name = "sigil-sdk-java"

include(":core")
include(":providers:openai")
include(":providers:anthropic")
include(":providers:gemini")
include(":benchmarks")

project(":providers:openai").projectDir = file("providers/openai")
project(":providers:anthropic").projectDir = file("providers/anthropic")
project(":providers:gemini").projectDir = file("providers/gemini")

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
