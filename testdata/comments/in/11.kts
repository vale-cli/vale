// A script's leading comment.
plugins {
    kotlin("jvm") version "2.0.0" /* inline block */
}

/** One line of KDoc. */
val url = "https://example.com // not a comment"
