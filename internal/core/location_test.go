package core

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/vale-cli/vale/v3/internal/nlp"
)

// A punctuation-only match in a block whose text was altered by inline markup
// (e.g. a stripped code span) must still be located within that block, not at
// the first occurrence anywhere in the document -- see #994.
func TestInitialPositionPunctAnchor(t *testing.T) {
	// ctx keeps the raw code span; txt is the extracted sentence without it.
	ctx := "Test, line.\nLine, with, four, commas, `yes`.\n"
	txt := "Line, with, four, commas, yes."

	pos, sub := initialPosition(ctx, txt, Alert{Match: ","})
	// The comma belongs to the second sentence (after "Line"), not the first
	// comma in "Test,". Position is 1-based rune count.
	if pos != 17 {
		t.Errorf("pos = %d, want 17 (second sentence)", pos)
	}
	if sub != "," {
		t.Errorf("sub = %q, want %q", sub, ",")
	}
}

// A match butted directly against a code span's delimiter is inside (or
// beside) that span, and locating an alert there mislocates it -- the prose
// occurrence the rule actually matched sits later in the line. fs[1] is
// exclusive, so the character to inspect is ctx[fs[1]] itself. See #1132's
// follow-up: `Inline `+"`sum(x) # ZQX`"+` and text ZQX after.`
func TestInsideInlineMarkup(t *testing.T) {
	cases := []struct {
		name string
		ctx  string
		fs   []int
		want bool
	}{
		{"plain prose", "some ZQX here", []int{5, 8}, false},
		{"opening backtick beside match", "x `ZQX` y", []int{3, 6}, true},
		{"closing backtick against match", "x `f() # ZQX` and ZQX y",
			[]int{9, 12}, true},
		{"prose occurrence after a span", "x `f() # ZQX` and ZQX y",
			[]int{18, 21}, false},
		{"hyphenated compound is not markup", "a well-known fix",
			[]int{2, 6}, false},
		{"opening math delimiter beside match", "x $ZQX$ y", []int{3, 6}, true},
		{"closing math delimiter against match", "x $f(ZQX)$ and ZQX y",
			[]int{5, 8}, false},
		{"prose occurrence after math", "x $ZQX$ and ZQX y",
			[]int{12, 15}, false},
		{"closing backtick past a multi-byte prefix", "éééééééé x ZQX.` y",
			[]int{19, 22}, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := insideInlineMarkup(c.ctx, c.fs); got != c.want {
				t.Errorf("insideInlineMarkup(%q, %v) = %v, want %v",
					c.ctx, c.fs, got, c.want)
			}
		})
	}
}

// A match shadowed by an identical string inside a code span must be located
// at its prose occurrence, not the span's.
func TestInitialPositionSkipsCodeSpan(t *testing.T) {
	ctx := "Inline `sum(x) # ZQX` and text ZQX after."
	txt := "Inline ************ and text ZQX after."

	pos, _ := initialPosition(ctx, txt, Alert{Match: "ZQX"})
	if pos != 32 {
		t.Errorf("pos = %d, want 32 (the prose occurrence)", pos)
	}
}

// A match preceded, within its block, by copies of its own text must be
// located at its own occurrence, not the first. skipOcc carries the count,
// measured by AddAlert where the span and the block text share a coordinate
// system -- the word-masking this replaced sliced a document-sized context at
// a block-sized span, and was capped at contexts under 1,000 bytes.
func TestInitialPositionSkipsPriorOccurrences(t *testing.T) {
	ctx := "One and two and three and four."

	for skip, want := range map[int]int{0: 5, 1: 13, 2: 23} {
		a := Alert{Match: "and", Span: []int{0, 0}, skipOcc: skip}
		if pos, _ := initialPosition(ctx, "unfindable block text", a); pos != want {
			t.Errorf("skip %d: pos = %d, want %d", skip, pos, want)
		}
	}
}

func TestCountPrior(t *testing.T) {
	cases := []struct {
		text string
		sub  string
		want int
	}{
		{"say and stay", "and", 1},
		{"band width", "and", 0},       // no boundary
		{"`and` beside and", "and", 1}, // inline code doesn't count
		{"and one and two and ", "and", 3},
		{"nothing here", "and", 0},
		{"", "and", 0},
	}

	for _, c := range cases {
		if got := countPrior(c.text, c.sub); got != c.want {
			t.Errorf("countPrior(%q, %q) = %d, want %d", c.text, c.sub, got, c.want)
		}
	}
}

func TestIsPunctOnly(t *testing.T) {
	cases := map[string]bool{
		",":      true,
		"...":    true,
		"":       false,
		"hav":    false,
		"that's": false,
		"OAuth2": false,
	}
	for in, want := range cases {
		if got := isPunctOnly(in); got != want {
			t.Errorf("isPunctOnly(%q) = %v, want %v", in, got, want)
		}
	}
}

// A match whose smart apostrophe/quote was normalized to ASCII (as
// spell-checking does) must still be located in the original source. Before
// the fix, the straight-apostrophe match couldn't be found in smart-apostrophe
// text, so the alert was dropped -- see #1003.
func TestInitialPositionSmartApostrophe(t *testing.T) {
	straight := "The toolkit's plugin." // a.Match is always normalized
	smart := "The toolkit’s plugin."    // source keeps the smart form

	tests := []struct {
		name string
		ctx  string
	}{
		{"straight source", straight},
		{"smart source", smart},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pos, sub := initialPosition(tt.ctx, tt.ctx, Alert{Match: "toolkit's"})
			if pos != 5 {
				t.Errorf("pos = %d, want 5", pos)
			}
			if sub != "toolkit's" {
				t.Errorf("sub = %q, want %q", sub, "toolkit's")
			}
		})
	}
}

// searchPosition is the position the pattern search reports -- what
// directPosition has to agree with whenever it accepts an offset.
func searchPosition(ctx, sub string) (int, bool) {
	pat := regexp.MustCompile(
		`(?:^|\b|_)` + quoteTolerantPattern(sub) + `(?:_|\b|$)`)

	fs := pat.FindStringIndex(ctx)
	if fs == nil {
		return 0, false
	}

	idx := fs[0]
	if strings.HasPrefix(ctx[idx:], "_") {
		idx++
	}

	return idx, true
}

// directPosition trades a search for arithmetic, so its whole value rests on
// never accepting an offset the search would have disagreed with. Two ways to
// get that wrong are easy to reach and hard to see: quote tolerance runs one
// way only (an ASCII apostrophe in the match may stand for a smart one in the
// source, never the reverse), and the pattern's `_` alternatives *consume* the
// underscore rather than looking past it, so it lands inside the match and
// cannot begin the next one.
func FuzzDirectPosition(f *testing.F) {
	f.Add("the quick brown fox", "brown", 4)
	f.Add("a_word_here", "word", 2)
	f.Add("_leading", "leading", 1)
	f.Add("the toolkit’s plugin", "toolkit's", 4)
	f.Add("the toolkit's plugin", "toolkit’s", 4)
	f.Add("alpha beta alpha", "alpha", 11)
	f.Add("@@@@@ word", "word", 6)
	f.Add("`code` word", "word", 7)
	f.Add("'_0ZaAZ _", "0ZaAZ", 2)

	f.Fuzz(func(t *testing.T, ctx, match string, idx int) {
		sub := strings.ToValidUTF8(match, "")
		if !utf8.ValidString(ctx) || sub == "" {
			t.Skip()
		}

		// from=0 asks the strongest question: is this the first match in the
		// whole context, which is exactly what the search returns.
		if !directPosition(ctx, idx, 0, sub) {
			t.Skip()
		}

		want, found := searchPosition(ctx, sub)
		if !found {
			t.Fatalf("accepted %d in %q for %q, but the search finds nothing",
				idx, ctx, sub)
		}
		if want != idx {
			t.Fatalf("accepted %d in %q for %q, but the search reports %d",
				idx, ctx, sub, want)
		}
	})
}

// locByScan is the previous implementation: count newlines from the start of
// the document on every call. The indexed version has to agree with it exactly.
func locByScan(ctx string, begin, end, pad int) (int, []int) {
	line := 1
	lineStart := 0

	for i := 0; i < begin && i < len(ctx); i++ {
		if ctx[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}

	col := nlp.StrLen(ctx[lineStart:begin]) + 1 + pad
	matchLen := nlp.StrLen(ctx[begin:end])

	span := []int{col, col + matchLen - 1}
	if span[1] < span[0] {
		span[1] = span[0]
	}

	return line, span
}

func TestLocFromByteOffsetMatchesScan(t *testing.T) {
	inputs := []string{
		"",
		"one line",
		"a\nb\nc",
		"\n\n\nleading blanks",
		"trailing\n",
		"héllo\nwörld\n日本語\n👍🏽 emoji",
		"win\r\nline\r\nendings",
	}

	f := &File{}
	for _, ctx := range inputs {
		starts := f.lineStarts(ctx)
		for begin := 0; begin <= len(ctx); begin++ {
			for end := begin; end <= len(ctx); end++ {
				for _, pad := range []int{0, 3} {
					wantLine, wantSpan := locByScan(ctx, begin, end, pad)
					gotLine, gotSpan := locFromByteOffset(ctx, starts, begin, end, pad)

					if gotLine != wantLine {
						t.Fatalf("ctx=%q begin=%d: line = %d, want %d",
							ctx, begin, gotLine, wantLine)
					}
					if gotSpan[0] != wantSpan[0] || gotSpan[1] != wantSpan[1] {
						t.Fatalf("ctx=%q begin=%d end=%d: span = %v, want %v",
							ctx, begin, end, gotSpan, wantSpan)
					}
				}
			}
		}
	}
}

// The index is cached per context; a different context must rebuild it.
func TestLineStartsRebuildsOnNewContext(t *testing.T) {
	f := &File{}

	if got := f.lineStarts("a\nb"); len(got) != 2 {
		t.Fatalf("lineStarts = %v, want 2 entries", got)
	}
	if got := f.lineStarts("a\nb\nc\nd"); len(got) != 4 {
		t.Fatalf("lineStarts after change = %v, want 4 entries", got)
	}
	if got := f.lineStarts("a\nb"); len(got) != 2 {
		t.Fatalf("lineStarts back = %v, want 2 entries", got)
	}
}

// The running column count must agree with counting from the line's start,
// whether alerts arrive in order along a line, jump back, or change lines.
func TestByteLocMatchesScan(t *testing.T) {
	inputs := []string{
		"one line",
		"héllo\nwörld\n日本語\n👍🏽 emoji",
		"a\nb\nc",
		"win\r\nline\r\nendings",
	}

	for _, ctx := range inputs {
		f := &File{}
		var begins []int
		for begin := 0; begin <= len(ctx); begin++ {
			begins = append(begins, begin)
		}
		// Forward, then backward, then forward again: every branch of the
		// cache in one context.
		order := append([]int{}, begins...)
		for i := len(begins) - 1; i >= 0; i-- {
			order = append(order, begins[i])
		}
		order = append(order, begins...)

		for _, begin := range order {
			end := min(begin+2, len(ctx))
			for _, pad := range []int{0, 3} {
				wantLine, wantSpan := locByScan(ctx, begin, end, pad)
				gotLine, gotSpan := f.byteLoc(ctx, begin, end, pad)

				if gotLine != wantLine {
					t.Fatalf("ctx=%q begin=%d: line = %d, want %d",
						ctx, begin, gotLine, wantLine)
				}
				if gotSpan[0] != wantSpan[0] || gotSpan[1] != wantSpan[1] {
					t.Fatalf("ctx=%q begin=%d end=%d: span = %v, want %v",
						ctx, begin, end, gotSpan, wantSpan)
				}
			}
		}
	}
}

// A new context restarts the count even when a position repeats.
func TestByteLocAcrossContexts(t *testing.T) {
	f := &File{}

	f.byteLoc("aaaa", 3, 4, 0)
	if _, span := f.byteLoc("éééé", 4, 6, 0); span[0] != 3 {
		t.Fatalf("got column %d, want 3", span[0])
	}
}

// A block the walker placed is located where it was placed, even when the
// same text occurs earlier; and the match is masked where it was found,
// not at its first occurrence as a substring of another word.
func TestLocateMatchTrustsPlacement(t *testing.T) {
	ctx := "| Name |\n\n| Name |\n"
	pos, _, hit := locateMatch(ctx, "Name", Alert{Match: "Name", Span: []int{0, 4}}, 12)
	if pos != 13 || hit != 12 {
		t.Errorf("got pos %d hit %d, want 13 and 12", pos, hit)
	}

	got := maskMatch("Subtitles.\nSay titl.\n", "titl", 15)
	if got != "Subtitles.\nSay ####.\n" {
		t.Errorf("maskMatch = %q", got)
	}
	if fallback := maskMatch("Say titl.\n", "titl", -1); fallback != "Say ####.\n" {
		t.Errorf("maskMatch fallback = %q", fallback)
	}
}

// A block's later matches are located in that block, not in a later copy
// of its text, although the earlier matches have been masked out of it.
func TestLocateMatchThroughMask(t *testing.T) {
	ctx := "######## here and iptables again.\n\niptables here and iptables again.\n"
	txt := "iptables here and iptables again."
	pos, _, hit := locateMatch(ctx, txt, Alert{Match: "iptables", Span: []int{18, 26}, skipOcc: 1}, 0)
	if pos != 19 || hit != 18 {
		t.Errorf("got pos %d hit %d, want 19 and 18", pos, hit)
	}
}

// A mask keeps the rune count and not the byte count, so a block whose
// earlier match was a multi-byte rune must still be found at its offset, and
// the offset itself has to be carried over to the shortened context. See #1184.
func TestLocateMatchThroughMultiByteMask(t *testing.T) {
	ctx := "\"'’\n\n”’"
	txt := "”’"

	masked := maskMatch(ctx, "”", 7)
	if masked != "\"'’\n\n#’" {
		t.Fatalf("maskMatch = %q", masked)
	}
	if !throughMask(masked[7:], txt) {
		t.Errorf("throughMask rejected %q against %q", masked[7:], txt)
	}
	if throughMask("#x", "”y") {
		t.Error("throughMask accepted a differing rune after a mask")
	}

	at := maskedOffset(ctx, masked, 7)
	if at != 7 {
		t.Errorf("maskedOffset = %d, want 7", at)
	}
	if got := maskedOffset(ctx, masked, 13); got != 11 {
		t.Errorf("maskedOffset at the end = %d, want 11", got)
	}
	if got := maskedOffset(ctx, ctx, 7); got != 7 {
		t.Errorf("maskedOffset on an unmasked copy = %d, want 7", got)
	}

	// The second `’` is the one in the block, not the one on line 1.
	pos, _, hit := locateMatch(masked, txt, Alert{Match: "’", Span: []int{3, 6}}, at)
	if pos != 7 || hit != 8 {
		t.Errorf("got pos %d hit %d, want 7 and 8", pos, hit)
	}
}
