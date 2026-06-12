package code

import (
	"bytes"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

type QueryEngine struct {
	tree   *sitter.Tree
	lang   *Language
	cutset string
}

func NewQueryEngine(tree *sitter.Tree, lang *Language) *QueryEngine {
	cutset := lang.Cutset
	if cutset == "" {
		cutset = " "
	}

	return &QueryEngine{
		tree:   tree,
		lang:   lang,
		cutset: cutset,
	}
}

func (qe *QueryEngine) run(meta string, q *sitter.Query, source []byte) []Comment {
	var comments []Comment

	if meta != "" {
		meta = "." + meta
	}

	qc := sitter.NewQueryCursor()
	qc.Exec(q, qe.tree.RootNode())

	for {
		m, ok := qc.NextMatch()
		if !ok {
			break
		}

		m = qc.FilterPredicates(m, source)
		for _, c := range m.Captures {
			rText := c.Node.Content(source)
			cText := qe.lang.Delims.ReplaceAllString(rText, "")

			scope := "text.comment" + meta + ".line"
			if strings.Count(cText, "\n") > 1 {
				scope = "text.comment" + meta + ".block"

				// Dedent like Python's inspect.cleandoc: the first line sits on
				// (or just after) the opening delimiter, so its leading
				// whitespace is incidental and is trimmed fully; the remaining
				// lines are dedented only by the indentation common to them.
				// This removes a comment's base indentation while preserving
				// relative indentation, which is significant for markup such as
				// RST literal blocks and Markdown indented code. See #1028.
				lines := strings.Split(cText, "\n")
				common := commonIndent(lines[1:], qe.cutset)

				buf := bytes.Buffer{}
				for i, line := range lines {
					if i == 0 {
						buf.WriteString(strings.TrimLeft(line, qe.cutset))
					} else {
						buf.WriteString(stripIndent(line, common, qe.cutset))
					}
					buf.WriteString("\n")
				}

				cText = buf.String()
			}

			comments = append(comments, Comment{
				Line:   int(c.Node.StartPoint().Row) + 1,
				Offset: int(c.Node.StartPoint().Column),
				Scope:  scope,
				Text:   cText,
				Source: rText,
			})
		}
	}

	return comments
}

// commonIndent returns the length, in bytes, of the longest run of leading
// `cutset` characters shared by every non-blank line. Blank lines (those that
// are empty after trimming) are ignored so they don't force the indent to zero.
func commonIndent(lines []string, cutset string) int {
	common := -1
	for _, line := range lines {
		if strings.TrimLeft(line, cutset) == "" {
			continue
		}
		n := len(line) - len(strings.TrimLeft(line, cutset))
		if common == -1 || n < common {
			common = n
		}
	}
	if common < 0 {
		return 0
	}
	return common
}

// stripIndent removes up to `n` leading `cutset` characters from `line` (a line
// with fewer than `n` leading cutset characters -- e.g. a blank line -- loses
// only what it has).
func stripIndent(line string, n int, cutset string) string {
	if n <= 0 {
		return line
	}
	cut := len(line) - len(strings.TrimLeft(line, cutset))
	if cut > n {
		cut = n
	}
	return line[cut:]
}
