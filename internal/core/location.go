package core

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/vale-cli/vale/v3/internal/nlp"
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
// literalNth finds the first occurrence of sub in ctx that the search pattern
// would accept, after skipping `skip` acceptable occurrences -- the match may
// be preceded, within its own block, by that many copies of its text. A
// candidate inside inline markup is neither counted nor returned; it is what
// the search is trying not to land on.
//
// The pattern is `(?:^|\b|_)` + sub + `(?:_|\b|$)`. When sub holds no quotes
// it is a plain literal, so the same answer comes from a literal search plus a
// boundary test at each hit -- and a literal search is the one thing the stdlib
// does with SIMD. Running the NFA over the whole context instead was 42% of a
// 890 KB file's lint.
//
// Boundary here means what `\b` means to the engine: a transition between a
// word character and something else. `_` is accepted explicitly on either side
// because the pattern spells it out, and the ends of the string count.
func literalNth(ctx, sub string, skip int) int {
	if sub == "" {
		return -1
	}

	first, _ := utf8.DecodeRuneInString(sub)
	last, _ := utf8.DecodeLastRuneInString(sub)

	for i := 0; ; {
		j := strings.Index(ctx[i:], sub)
		if j < 0 {
			return -1
		}
		at := i + j

		if boundaryBefore(ctx, at, first) && boundaryAfter(ctx, at+len(sub), last) &&
			!insideInlineMarkup(ctx, []int{at, at + len(sub)}) {
			if skip == 0 {
				return at
			}
			skip--
		}
		// Advance by one byte rather than by len(sub): overlapping candidates
		// are rare but real, and skipping them would miss a valid match.
		_, w := utf8.DecodeRuneInString(ctx[at:])
		i = at + w
	}
}

// countPrior counts the occurrences of sub in text that the search would
// accept, judged the same way literalNth judges its candidates -- an
// occurrence the search would never land on must not widen the skip.
func countPrior(text, sub string) int {
	if sub == "" {
		return 0
	}

	first, _ := utf8.DecodeRuneInString(sub)
	last, _ := utf8.DecodeLastRuneInString(sub)

	found := 0
	for i := 0; ; {
		j := strings.Index(text[i:], sub)
		if j < 0 {
			return found
		}
		at := i + j

		if boundaryBefore(text, at, first) && boundaryAfter(text, at+len(sub), last) &&
			!insideInlineMarkup(text, []int{at, at + len(sub)}) {
			found++
		}
		_, w := utf8.DecodeRuneInString(text[at:])
		i = at + w
	}
}

// boundaryBefore reports whether a match starting at `at` sits on a boundary.
func boundaryBefore(ctx string, at int, first rune) bool {
	if at == 0 {
		return true
	}
	prev, _ := utf8.DecodeLastRuneInString(ctx[:at])
	if prev == '_' {
		return true
	}
	return joinsWord(prev) != joinsWord(first)
}

// boundaryAfter reports whether a match ending at `at` sits on a boundary.
func boundaryAfter(ctx string, at int, last rune) bool {
	if at >= len(ctx) {
		return true
	}
	next, _ := utf8.DecodeRuneInString(ctx[at:])
	if next == '_' {
		return true
	}
	return joinsWord(next) != joinsWord(last)
}

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

// insideInlineMarkup reports whether a match sits against a backtick or a
// dash, which is how a match inside inline code is recognised.
//
// NOTE: This is a workaround for #673. Ideally we'd handle it at the AST level
// by ignoring inline code spans.
func insideInlineMarkup(ctx string, fs []int) bool {
	start := fs[0] - 1
	end := fs[1] + 1
	if start > 0 && (ctx[start] == '`' || ctx[start] == '-') {
		return true
	} else if end < len(ctx) && (ctx[end] == '`' || ctx[end] == '-') &&
		!unicode.IsSpace(rune(ctx[fs[1]])) {
		// `end` looks one past the character beside the match, catching a
		// closing backtick separated by punctuation (`foo word.`). When the
		// character between is whitespace, though, the backtick *opens* a code
		// span after the match -- `word `code`` is prose sitting before code,
		// and rejecting it hands the alert to a later occurrence (#1147).
		return true
	}

	// fs[1] is exclusive, so the character right beside the match is
	// ctx[fs[1]] -- the check above looks one further out and misses a
	// closing backtick sitting directly against the match, as in
	// `f(x) # word`. Only backticks: a hyphen there is usually a compound
	// word, and rejecting `well` in `well-known` would mislocate it.
	if fs[1] < len(ctx) && ctx[fs[1]] == '`' {
		return true
	}

	// `$…$` inline math is skipped the same way inline code is, so a match
	// pressed against a delimiter is markup rather than the prose that fired.
	// Without this, a word inside an equation wins over the same word later in
	// the line and the alert lands in the math. See #839.
	if fs[0] > 0 && ctx[fs[0]-1] == '$' {
		return true
	} else if fs[1] < len(ctx) && ctx[fs[1]] == '$' {
		return true
	}

	return false
}

// joinsWord reports whether r would sit inside a word, and so cannot stand
// beside a match that the pattern below requires to be `\b`-bounded.
//
// `_` is a word character to the regex engine, but the pattern admits it as a
// boundary of its own, so it is not one of these.
func joinsWord(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// directPosition confirms that `idx` is where the search below would land, so
// that the search itself can be skipped.
//
// A check knows where in its block the match was, and the block knows where it
// begins in `ctx`, so the position is arithmetic. That matters because the
// search runs once per alert over the whole context, and spelling reports
// alerts by the thousand -- the difference between linear and quadratic on a
// large document.
//
// The arithmetic rests on hints rather than promises, though: a check may
// measure against text it transformed first (spelling normalizes quotes before
// tokenizing), and `ctx` may have been masked by earlier alerts. So the answer
// is only used when the bytes there really are the match, bounded as the
// pattern requires, with nothing before it the pattern would have matched
// first. `from` is where that last check has to start -- everything earlier is
// already known to hold no match. Anything short of all three falls through to
// the search, which makes a bad hint cost time instead of correctness.
func directPosition(ctx string, idx, from int, sub string) bool {
	// Quote tolerance and the `^`/`$`/`_` alternatives make the pattern's
	// boundaries subtle in both directions. Restricting this to matches that
	// begin and end on a letter or digit and hold no quotes covers the ordinary
	// word -- what spelling reports -- and leaves the rest to the search.
	if sub == "" || from < 0 || idx < from || strings.ContainsAny(sub, `'"`) {
		return false
	}
	if first, _ := utf8.DecodeRuneInString(sub); !joinsWord(first) {
		return false
	}
	if last, _ := utf8.DecodeLastRuneInString(sub); !joinsWord(last) {
		return false
	}

	end := idx + len(sub)
	if end > len(ctx) || ctx[idx:end] != sub {
		return false
	}

	if idx > 0 {
		if r, _ := utf8.DecodeLastRuneInString(ctx[:idx]); joinsWord(r) {
			return false
		}
	}
	if end < len(ctx) {
		if r, _ := utf8.DecodeRuneInString(ctx[end:]); joinsWord(r) {
			return false
		}
	}

	// The search reports the *first* acceptable match, so this agrees with it
	// only if there is nothing earlier to find. A plain scan is enough: `sub`
	// holds no quote for the pattern to be tolerant about, so the pattern can
	// only match where `sub` itself occurs.
	return !strings.Contains(ctx[from:idx], sub)
}

// throughMask reports whether ctx begins with txt, a masked rune in ctx
// standing for any rune of txt. Rune by rune: a mask keeps the rune count,
// not the byte count, since a multi-byte rune is masked as a single `#`.
func throughMask(ctx, txt string) bool {
	for _, want := range txt {
		if ctx == "" {
			return false
		}
		got, n := utf8.DecodeRuneInString(ctx)
		if got != want && got != '#' && got != '@' {
			return false
		}
		ctx = ctx[n:]
	}
	return true
}

// maskedOffset maps the byte offset at in ctx to the same rune in masked, a
// copy of ctx that masking has shortened. The runes still line up, so the
// offset is carried over by rune count.
func maskedOffset(ctx, masked string, at int) int {
	if at <= 0 || at > len(ctx) || len(ctx) == len(masked) {
		return at
	}
	runes := utf8.RuneCountInString(ctx[:at])
	for i := range masked {
		if runes == 0 {
			return i
		}
		runes--
	}
	return len(masked)
}

// located is positionOf with the match's byte index in the unmasked context.
func located(ctx string, idx int, sub string, shift int) (int, string, int) {
	if strings.HasPrefix(ctx[idx:], "_") {
		idx++
	}
	return nlp.StrLen(ctx[:idx]) + 1, sub, idx + shift
}

// maskMatch masks the match at hit in ctx, rune for rune, or its first
// occurrence when hit is not where it was found.
func maskMatch(ctx, match string, hit int) string {
	end := hit + len(match)
	if hit < 0 || end > len(ctx) || ctx[hit:end] != match {
		masked, _ := Substitute(ctx, match, '#')
		return masked
	}
	return ctx[:hit] + strings.Map(func(r rune) rune {
		if r != '\n' {
			return '#'
		}
		return r
	}, match) + ctx[end:]
}

// initialPosition calculates the position of a match (given by the location in
// the reference document, `loc`) in the source document (`ctx`).
//
// `at` is where `txt` begins in `ctx` if the caller knows, and -1 otherwise. It
// is only a shortcut: the position it produces is checked before being used.
func initialPosition(ctx, txt string, a Alert) (int, string) {
	pos, sub, _ := locateMatch(ctx, txt, a, -1)
	return pos, sub
}

// locateMatch is initialPosition with the byte index of the match in ctx,
// or -1 when the position was guessed rather than found.
func locateMatch(ctx, txt string, a Alert, at int) (int, string, int) {
	var idx int
	var pat *regexp.Regexp

	if a.Match == "" {
		// We have nothing to look for -- assume the rule applies to the entire
		// document (e.g., readability).
		return 1, "", -1
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
	// The walker placed the block; trust that over an earlier copy of its
	// text, which is what a repeated table header or heading is. Earlier
	// alerts have masked their matches in the block, so the text is read
	// through the mask.
	if at >= 0 && at <= len(ctx) && throughMask(ctx[at:], txt) {
		offset = at
	}

	// The prefix mask below maps rune to rune, so a prefix holding anything
	// multi-byte comes back shorter; shift maps an index in the masked text
	// back to ctx as given.
	shift := 0
	if offset >= 0 {
		masked, _ := Substitute(ctx, ctx[:offset], '@')
		// The tail it leaves alone is what gives the block its new position.
		offset = len(masked) - (len(ctx) - offset)
		shift = len(ctx) - len(masked)
		ctx = masked
	}

	sub := strings.ToValidUTF8(a.Match, "")

	// Where the block starts in `ctx`, and how far the pattern is already known
	// to have nothing to match. The search above answers both at once. When it
	// came up empty -- which is what happens once an earlier alert has masked
	// part of this block, so on every alert but the first -- the caller's offset
	// still says where the block is, but nothing has ruled out the text in front
	// of it, so that has to be scanned as well.
	base, from := offset, offset
	if base < 0 {
		base, from = at, 0
	}

	if base >= 0 && len(a.Span) > 0 && a.Span[0] >= 0 {
		if pos := base + a.Span[0]; directPosition(ctx, pos, from, sub) {
			// The span the pattern would have reported, so that the inline-code
			// test below sees what it sees: both `_` alternatives are consumed
			// by the match rather than left beside it.
			fs := []int{pos, pos + len(sub)}
			if pos > 0 && ctx[pos-1] == '_' {
				fs[0]--
			}
			if fs[1] < len(ctx) && ctx[fs[1]] == '_' {
				fs[1]++
			}
			if !insideInlineMarkup(ctx, fs) {
				return located(ctx, pos, sub, shift)
			}
		}
	}

	pat = regexp.MustCompile(`(?:^|\b|_)` + quoteTolerantPattern(sub) + `(?:_|\b|$)`)

	// The match may be preceded, within its own block, by copies of its text;
	// the search skips exactly that many acceptable candidates so a repeated
	// word resolves to the occurrence that actually matched.
	skip := a.skipOcc

	// Only the first acceptable match is used, so the search stops at the
	// first one and widens only if that turns out to be rejected below. This
	// runs once per alert over the whole context, so scanning past a match
	// nothing reads is the difference between linear and quadratic on a
	// document with many alerts -- which spelling produces by the thousand.
	// A quote-free match is a plain literal, so the first candidate can be
	// found without running the engine over the whole context.
	if !strings.ContainsAny(sub, `'"`) {
		if lit := literalNth(ctx, sub, skip); lit >= 0 {
			return located(ctx, lit, sub, shift)
		}
	}

	fsi := pat.FindAllStringIndex(ctx, 1)
	if len(fsi) > 0 && skip == 0 && !insideInlineMarkup(ctx, fsi[0]) {
		return located(ctx, fsi[0][0], sub, shift)
	}
	if len(fsi) > 0 {
		fsi = pat.FindAllStringIndex(ctx, -1)
	}
	if len(fsi) == 0 {
		idx = strings.Index(ctx, sub)
		if idx < 0 {
			// This should only happen if we're in a scope that contains inline
			// markup (e.g., a sentence with code spans).
			p, g := guessLocation(ctx, txt, sub)
			return p, g, -1
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
		for _, fs := range fsi {
			if insideInlineMarkup(ctx, fs) {
				continue
			}
			if skip > 0 {
				skip--
				continue
			}
			idx = fs[0]
			break
		}
	}

	return located(ctx, idx, sub, shift)
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
