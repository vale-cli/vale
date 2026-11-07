package code

import (
	"bytes"
	"sort"
	"strings"
)

// Comment represents an in-code comment (line or block).
type Comment struct {
	Text   string
	Source string
	Line   int
	Offset int
	Scope  string
	// Strip records, for each line of Text, how many bytes were taken off the
	// front of the matching Source line: the opener, the dedent, and a line
	// comment's own decoration. An alert's column is meaningful in Text;
	// putting it back in Source means adding this.
	//
	// Not serialised: this is bookkeeping for putting an alert back where it
	// came from, not part of what a comment is.
	Strip []int `json:"-"`
	// Format is the markup the comment is written in, from the query's
	// `type`; empty for plain lines.
	Format string `json:"-"`
	// Doc is the name the comment is expected to open with, when it
	// documents a declaration. See Language.DocName.
	Doc string `json:"-"`
	// Masks are the ranges of Text that hold no prose; an alert whose match
	// lies inside one is dropped. See doc.go.
	Masks []Mask `json:"-"`
}

// StripAt returns what came off the front of a line, 1-based as alerts are.
func (c Comment) StripAt(line int) (int, bool) {
	if line < 1 || line > len(c.Strip) {
		return 0, false
	}
	return c.Strip[line-1], true
}

// doneMerging determines when we should *stop* concatenating line-scoped
// comments.
func doneMerging(curr, prev Comment) bool {
	switch {
	case prev.Line != curr.Line-1:
		// If the comments aren't on consecutive lines, don't merge them.
		return true
	case prev.Offset != curr.Offset:
		// If the comments aren't at the same offset, don't merge them.
		return true
	case prev.Format != curr.Format:
		// Two queries with different markup: each is read on its own.
		return true
	}
	return false
}

// appendLine adds one line to a pending run of line comments.
//
// One newline per line, so the run ends up with exactly as many lines as the
// source it came from. A blank line comment -- `//` with nothing after it --
// is empty and contributes only its newline; giving it two ended that line
// twice and put a line in the extracted text that the source doesn't have.
// See #1022.
func appendLine(line string) string {
	// The space after the delimiter belongs to the delimiter, and the padding
	// added when an alert is mapped back already counts it. Trimming only the
	// lines that still needed a newline left it on the ones that didn't --
	// Rust's `///`, whose node content carries its own -- where it was then
	// counted twice. The first line of a run is trimmed by the caller.
	line = strings.TrimLeft(line, " ")
	return strings.TrimRight(line, "\n") + "\n"
}

// attachRun joins a pending run of lines to the text it belongs to.
//
// The run needs the line above it terminated, which is a question about that
// text and not about the run: a comment whose node content already carried its
// newline -- Rust's `//!`, for one -- is terminated, and adding another puts a
// blank line between the two that the source doesn't have. Keying this off the
// run instead lost the terminator whenever the run began with a blank comment
// line, which cost a line in the other direction.
func attachRun(text, run string) string {
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}

	return text + run
}

func coalesce(comments []Comment) []Comment {
	var joined []Comment

	tBuf := bytes.Buffer{}
	sBuf := bytes.Buffer{}

	// flush merges any pending line-comment text into the most recently
	// appended comment, which is always the one this run of lines belongs to.
	flush := func() {
		if tBuf.Len() > 0 {
			last := joined[len(joined)-1]

			last.Text = attachRun(last.Text, tBuf.String())
			last.Source = attachRun(last.Source, sBuf.String())

			joined[len(joined)-1] = last

			tBuf.Reset()
			sBuf.Reset()
		}
	}

	for i, comment := range comments {
		if comment.Scope == "text.comment.block" { //nolint:gocritic
			// Flush first: a pending run of line comments belongs to the
			// preceding line comment, not this block. Without this, the
			// buffered text leaks into the block and is reported at the
			// block's line (past EOF) under the wrong scope -- see #1020.
			flush()
			joined = append(joined, comment)
		} else if i == 0 || doneMerging(comment, comments[i-1]) {
			flush()
			joined = append(joined, comment)
		} else {
			// The run's Source lines each begin at their own delimiter, so
			// what came off this one is its opener plus the space appendLine
			// trims.
			text := appendLine(comment.Text)
			trimmed := len(comment.Text) - len(strings.TrimLeft(comment.Text, " "))
			strip := trimmed
			if n, ok := comment.StripAt(1); ok {
				strip += n
			}
			joined[len(joined)-1].Strip = append(joined[len(joined)-1].Strip, strip)

			tBuf.WriteString(text)
			sBuf.WriteString(appendLine(comment.Source))
			// The declaration follows the run's last line, so its name
			// belongs to the run.
			if comment.Doc != "" {
				joined[len(joined)-1].Doc = comment.Doc
			}
		}
	}

	flush()

	for i, comment := range joined {
		trimmed := strings.TrimLeft(comment.Text, " ")

		// Trimming the first line moves it, so what came off it grows to
		// match. A block comment's first line is empty and this is a no-op.
		if n := len(comment.Text) - len(trimmed); n > 0 && len(comment.Strip) > 0 {
			joined[i].Strip[0] += n
		}

		joined[i].Text = trimmed
	}

	return joined
}

// GetComments returns all comments in the given source code.
func GetComments(source []byte, lang *Language) ([]Comment, error) {
	var comments []Comment

	grammar, err := Grammar(lang.Grammar)
	if err != nil {
		return comments, err
	}
	tree, err := grammar.Parse(source)
	if err != nil {
		return comments, err
	}
	defer tree.Close()
	engine := NewQueryEngine(tree, lang)

	for _, query := range lang.Queries {
		q, qErr := tree.Query(query.Expr)
		if qErr != nil {
			return comments, qErr
		}
		comments = append(comments, engine.run(query.Name, query.Type, q, source)...)
	}

	if len(lang.Queries) > 1 {
		sort.Slice(comments, func(p, q int) bool {
			return comments[p].Line < comments[q].Line
		})
	}

	joined := coalesce(dropDirectives(dropShebang(comments), lang))
	for i, c := range joined {
		if lang.Doc != nil {
			joined[i].Masks = lang.Doc(c.Text)
		}
		if m, ok := leadingMask(c.Text, c.Doc); ok {
			joined[i].Masks = append(joined[i].Masks, m)
		}
	}

	return joined, nil
}

// dropDirectives removes the comments addressed to a tool rather than a
// reader. See Language.Directive.
func dropDirectives(comments []Comment, lang *Language) []Comment {
	if lang.Directive == nil {
		return comments
	}
	kept := comments[:0:0]
	for _, c := range comments {
		if !lang.Directive.MatchString(c.Source) {
			kept = append(kept, c)
		}
	}
	return kept
}

// dropShebang removes a leading `#!` line.
//
// A shebang names the interpreter a script runs under. Languages that use `#`
// for comments give tree-sitter no way to tell the two apart, so it arrives
// here as a comment and is linted as prose -- a path like `/usr/bin/env` then
// draws alerts about words the author never wrote. See #631.
//
// Only the very first line of the file qualifies, which is also all the kernel
// accepts, so a `#!` written anywhere else stays a comment.
func dropShebang(comments []Comment) []Comment {
	for i, c := range comments {
		if c.Line != 1 || c.Offset != 0 {
			continue
		} else if !strings.HasPrefix(c.Source, "#!") {
			continue
		}
		return append(comments[:i:i], comments[i+1:]...)
	}
	return comments
}
