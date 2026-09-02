package check

import (
	"unicode"
	"unicode/utf8"
)

// The default spelling filters, hand-written.
//
// A spelling rule runs these against every word of every block, and blocks
// overlap -- a sentence is also part of a paragraph -- so the same word is
// tested several times over. Profiling a spell-check of a 120 KB file put 72%
// of the run inside regexp.(*machine).match, all of it here.
//
// The three patterns are simple enough to read directly. Order matters:
// skipsNonWord is both the cheapest and the most often true, so it goes first.

// skipsNonWord reports whether a word contains anything outside the pattern
// `[^\p{L}_']` -- that is, whether that pattern would match.
func skipsNonWord(word string) bool {
	for i := 0; i < len(word); i++ {
		char := word[i]
		if isUpper(char) || isLower(char) || char == '_' || char == '\'' {
			continue
		}
		if char < utf8.RuneSelf {
			return true
		}

		// Preserve the byte-scan fast path for ASCII words, then decode only
		// the suffix that actually contains Unicode.
		for _, r := range word[i:] {
			if !unicode.IsLetter(r) && r != '_' && r != '\'' {
				return true
			}
		}
		return false
	}
	return false
}

// skipsTrailingCaps reports whether `[\p{Lu}]+$` would match: the word ends in
// at least one Unicode uppercase letter.
func skipsTrailingCaps(word string) bool {
	if word == "" {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(word)
	return unicode.IsUpper(r)
}

// skipsCamel reports whether `[A-Z]{1}[a-z]+[A-Z]+\w+` would match: a capital,
// one or more lower-case letters, one or more capitals, then a word character.
//
// `[A-Z]+` backtracks, which is easy to miss: it can hand a capital back to
// `\w+`, so two capitals satisfy the tail on their own and nothing need follow
// them. A greedy scan that consumed every capital and then demanded another
// word character got "'_0ZaAZ _" wrong.
func skipsCamel(word string) bool {
	for i := 0; i < len(word); i++ {
		if !isUpper(word[i]) {
			continue
		}

		j := i + 1
		for j < len(word) && isLower(word[j]) {
			j++
		}
		if j == i+1 { // no lower-case run
			continue
		}

		k := j
		for k < len(word) && isUpper(word[k]) {
			k++
		}
		switch {
		case k-j >= 2:
			// Two capitals: one for `[A-Z]+`, one for `\w+`.
			return true
		case k-j == 1 && k < len(word) && isWordByte(word[k]):
			return true
		}
	}
	return false
}

func isUpper(c byte) bool { return c >= 'A' && c <= 'Z' }
func isLower(c byte) bool { return c >= 'a' && c <= 'z' }

// isWordByte answers `\w`, which is [0-9A-Za-z_] for an ASCII pattern.
func isWordByte(c byte) bool {
	return isUpper(c) || isLower(c) || (c >= '0' && c <= '9') || c == '_'
}

// skippedByDefault reports whether any default filter would skip the word.
func skippedByDefault(word string) bool {
	return skipsNonWord(word) || skipsTrailingCaps(word) || skipsCamel(word)
}
