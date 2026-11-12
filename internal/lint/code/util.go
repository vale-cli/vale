package code

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

func cStyle(s string) int {
	return computePadding(s, []string{"/*", "//"})
}

// cStylePrefix matches the ` *` that decorates each line of a JSDoc, Javadoc
// or Doxygen block.
//
// The `*` only counts as decoration when whitespace or the end of the line
// follows it. `*emphasis*` and `*item` are content, and a pattern that took
// them for decoration would quietly delete the markup it was meant to expose.
var cStylePrefix = regexp.MustCompile(`^([ \t]*\*)(?:[ \t]|$)`)

func computePadding(s string, makers []string) int {
	padding := 0

	// Markers repeat -- a `##` banner, Julia's `#=` opener against a plain
	// `#` -- so consume the longest at each step until prose begins. This has
	// to mirror what stripDelims took off, or the alert lands short.
	rest := s
	for {
		matched := ""
		for _, m := range makers {
			if strings.HasPrefix(rest, m) && len(m) > len(matched) {
				matched = m
			}
		}
		if matched == "" {
			break
		}
		padding += utf8.RuneCountInString(matched)
		rest = rest[len(matched):]
	}

	if padding == 0 {
		return 0
	}
	for _, r := range rest {
		if r != ' ' {
			break
		}
		padding++
	}

	return padding
}
