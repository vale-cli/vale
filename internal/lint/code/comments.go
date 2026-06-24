package code

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// Comment represents an in-code comment (line or block).
type Comment struct {
	Text   string
	Source string
	Line   int
	Offset int
	Scope  string
}

// doneMerging determines when we should *stop* concatenating line-scoped
// comments.
func doneMerging(curr, prev Comment) bool {
	if prev.Line != curr.Line-1 {
		// If the comments aren't on consecutive lines, don't merge them.
		return true
	} else if prev.Offset != curr.Offset {
		// If the comments aren't at the same offset, don't merge them.
		return true
	}
	return false
}

func addSourceLine(line string, atEnd bool) string {
	if line == "" {
		return "\n\n"
	}

	if !strings.HasPrefix(line, "\n") && !atEnd {
		line = strings.TrimLeft(line, " ")
		line = fmt.Sprintf("\n%s", line)
	} else if !strings.HasSuffix(line, "\n") && atEnd {
		line = strings.TrimLeft(line, " ")
		line = fmt.Sprintf("%s\n", line)
	}

	return line
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

			last.Text += addSourceLine(tBuf.String(), false)
			last.Source += addSourceLine(sBuf.String(), false)

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
			tBuf.WriteString(addSourceLine(comment.Text, true))
			sBuf.WriteString(addSourceLine(comment.Source, true))
		}
	}

	flush()

	for i, comment := range joined {
		joined[i].Text = strings.TrimLeft(comment.Text, " ")
	}

	return joined
}

// GetComments returns all comments in the given source code.
func GetComments(source []byte, lang *Language) ([]Comment, error) {
	var comments []Comment

	parser := sitter.NewParser()
	parser.SetLanguage(lang.Parser)

	tree, err := parser.ParseCtx(context.Background(), nil, source)
	if err != nil {
		return comments, err
	}
	engine := NewQueryEngine(tree, lang)

	for _, query := range lang.Queries {
		q, qErr := sitter.NewQuery([]byte(query.Expr), lang.Parser)
		if qErr != nil {
			return comments, qErr
		}
		comments = append(comments, engine.run(query.Name, q, source)...)
	}

	if len(lang.Queries) > 1 {
		sort.Slice(comments, func(p, q int) bool {
			return comments[p].Line < comments[q].Line
		})
	}

	return coalesce(comments), nil
}
