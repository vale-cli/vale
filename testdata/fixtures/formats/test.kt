// NOTE: a line comment
// XXX: continued on the next line
package test

/**
 * TODO: a KDoc block.
 *
 * @param name FIXME: a tag description.
 */
fun greet(name: String): String {
    /* NOTE: a block comment */
    val s = "TODO: a string, not a comment"
    return s // FIXME: a trailing comment
}

/*
 * XXX: a multi-line block comment.
 */
class Test

/** NOTE: a one-line KDoc. */
fun one() = 1
