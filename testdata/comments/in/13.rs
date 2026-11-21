/*! Crate-level docs in a block. */

/// A line doc comment.
fn main() {
    /* A plain block comment. */
    /* An outer comment /* with a nested one */ that keeps going. */
    /**
     * A block doc comment,
     * decorated on each line.
     */
    let s = "not /* a comment */ at all";
}
