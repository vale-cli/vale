package core

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/errata-ai/vale/v3/internal/nlp"
)

// isPunctOnly reports whether s contains no letters or digits -- e.g. a bare
// `,`. Such matches are inherently ambiguous (they occur all over a document),
// so locating them needs the block-masking anchor below.
func isPunctOnly(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return s != ""
}

// quoteTolerantPattern escapes s for use in a regex, but lets ASCII quotes and
// apostrophes also match their "smart" Unicode variants. Spell-checking (and
// other checks) normalize `’` -> `'` etc. before matching, so the resulting
// match must still be locatable in the original, un-normalized source. See
// #1003.
func quoteTolerantPattern(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\'':
			b.WriteString(`['\x{2018}\x{2019}]`)
		case '"':
			b.WriteString(`["\x{201c}\x{201d}]`)
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	return b.String()
}

// initialPosition calculates the position of a match (given by the location in
// the reference document, `loc`) in the source document (`ctx`).
func initialPosition(ctx, txt string, a Alert) (int, string) {
	var idx int
	var pat *regexp.Regexp

	if a.Match == "" {
		// We have nothing to look for -- assume the rule applies to the entire
		// document (e.g., readability).
		return 1, ""
	}

	offset := strings.Index(ctx, txt)
	if offset < 0 && isPunctOnly(a.Match) {
		// `txt` may not appear verbatim in `ctx` when inline markup (e.g. a
		// code span) was stripped during extraction. For a punctuation-only
		// match -- which occurs all over the document -- anchor on the block's
		// first token so we still mask everything before it; otherwise a bare
		// `,` is located at its first occurrence anywhere. Restricted to
		// punctuation matches to preserve word-match disambiguation. See #994.
		if fields := strings.Fields(txt); len(fields) > 0 {
			offset = strings.Index(ctx, fields[0])
		}
	}
	if offset >= 0 {
		ctx, _ = Substitute(ctx, ctx[:offset], '@')
	}

	sub := strings.ToValidUTF8(a.Match, "")
	pat = regexp.MustCompile(`(?:^|\b|_)` + quoteTolerantPattern(sub) + `(?:_|\b|$)`)

	fsi := pat.FindAllStringIndex(ctx, -1)
	if len(fsi) == 0 {
		idx = strings.Index(ctx, sub)
		if idx < 0 {
			// This should only happen if we're in a scope that contains inline
			// markup (e.g., a sentence with code spans).
			return guessLocation(ctx, txt, sub)
		}
	} else {
		idx = fsi[0][0]
		// NOTE: This is a workaround for #673.
		//
		// In cases where we have more than one match, we skip any that look
		// like they're inside inline code (e.g., `code`).
		//
		// This is a bit of a hack: ideally, we'd handle this at the AST level
		// by ignoring these inline code spans.
		//
		// TODO: What about `scope: raw`?
		size := nlp.StrLen(ctx)
		for _, fs := range fsi {
			start := fs[0] - 1
			end := fs[1] + 1
			if start > 0 && (ctx[start] == '`' || ctx[start] == '-') {
				continue
			} else if end < size && (ctx[end] == '`' || ctx[end] == '-') {
				continue
			}
			idx = fs[0]
			break
		}
	}

	if strings.HasPrefix(ctx[idx:], "_") {
		idx++ // We don't want to include the underscore boundary.
	}

	return nlp.StrLen(ctx[:idx]) + 1, sub
}

func guessLocation(ctx, sub, match string) (int, string) {
	target := ""
	for _, s := range nlp.SentenceTokenizer.Segment(sub) {
		if s == match || strings.Index(s, match) > 0 {
			target = s
		}
	}

	if target == "" {
		return -1, sub
	}

	tokens := nlp.WordTokenizer.Tokenize(target)
	for _, text := range strings.Split(ctx, "\n") {
		if allStringsInString(tokens, text) {
			return strings.Index(ctx, text) + 1, text
		}
	}

	return -1, sub
}

func allStringsInString(subs []string, s string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
