package check

import (
	"regexp"
	"slices"
	"strings"
)

// lineWrap is a run of whitespace that holds a line break.
var lineWrap = regexp.MustCompile(`[ \t]*\n\s*`)

// wrapSpaces returns pattern with each run of spaces between words matching
// a line wrap too, so a phrase in a rule is found however the paragraph was
// wrapped. A space inside a character class, after a backslash, or ahead of
// a quantifier keeps its meaning, as does every space in a free-spacing
// pattern and one that is a whole branch of an alternation. A raw-scoped
// rule is left alone: there a line break is markup, not a wrap.
func wrapSpaces(pattern string, scope []string) string {
	if slices.Contains(scope, "raw") {
		return pattern
	}
	if !strings.ContainsAny(pattern, " \t") || strings.Contains(pattern, "(?x") {
		return pattern
	}

	var b strings.Builder
	inClass := false
	// opens: the last byte written began a group or a branch.
	opens := false
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		opened := opens
		opens = false
		switch {
		case c == '\\' && i+1 < len(pattern):
			b.WriteByte(c)
			b.WriteByte(pattern[i+1])
			i++
		case inClass:
			if c == ']' {
				inClass = false
			}
			b.WriteByte(c)
		case c == '[':
			inClass = true
			b.WriteByte(c)
		case c == ' ' || c == '\t':
			j := i
			for j < len(pattern) && (pattern[j] == ' ' || pattern[j] == '\t') {
				j++
			}
			branch := opened && j < len(pattern) && (pattern[j] == '|' || pattern[j] == ')')
			if branch || (j < len(pattern) && strings.IndexByte("*+?{", pattern[j]) >= 0) {
				b.WriteString(pattern[i:j])
			} else {
				b.WriteString(`\s+`)
			}
			i = j - 1
		default:
			opens = c == '(' || c == '|'
			b.WriteByte(c)
		}
	}
	return b.String()
}

// unwrap joins the lines of a match that wraps across a line, so a message
// shows the phrase as written in the rule.
func unwrap(s string) string {
	if !strings.Contains(s, "\n") {
		return s
	}
	return lineWrap.ReplaceAllString(s, " ")
}
