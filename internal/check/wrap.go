package check

import (
	"regexp"
	"strings"
)

// lineWrap is a run of whitespace that holds a line break.
var lineWrap = regexp.MustCompile(`[ \t]*\n\s*`)

// wrapSpaces returns pattern with each run of spaces between words matching
// a line wrap too, so a phrase in a rule is found however the paragraph was
// wrapped; a group counts as a word. Any other space keeps its meaning.
func wrapSpaces(pattern string) string {
	if !strings.ContainsAny(pattern, " \t") || strings.Contains(pattern, "(?x") {
		return pattern
	}

	var b strings.Builder
	inClass := false
	// afterWord: the last byte written ended a word or a group.
	afterWord := false
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		wasWord := afterWord
		afterWord = false
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
			if wasWord && j < len(pattern) && (phraseByte(pattern[j]) || pattern[j] == '(') {
				b.WriteString(`\s+`)
			} else {
				b.WriteString(pattern[i:j])
			}
			i = j - 1
		default:
			afterWord = phraseByte(c) || c == ')'
			b.WriteByte(c)
		}
	}
	return b.String()
}

// phraseByte reports whether c can end or start a word in a phrase: a `\w`
// byte, or part of a multi-byte character.
func phraseByte(c byte) bool {
	return isWordByte(c) || c >= 0x80
}

// unwrap joins the lines of a match that wraps across a line, so a message
// shows the phrase as written in the rule.
func unwrap(s string) string {
	if !strings.Contains(s, "\n") {
		return s
	}
	return lineWrap.ReplaceAllString(s, " ")
}
