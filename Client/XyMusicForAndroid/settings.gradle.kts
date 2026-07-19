pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
    }
}

rootProject.name = "XyMusic"
include(":app")
include(":benchmark")
include(":core:model")
include(":core:database")
include(":core:network")
include(":core:preferences")
include(":core:ui")
include(":domain")
