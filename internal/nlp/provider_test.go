package nlp

import (
	"strings"
	"testing"
)

// Compute wraps a block's paragraphs as `paragraph.<scope>` only when told the
// block holds paragraphs. A heading or a table cell is segmented like any
// other prose, but a rule scoped to `paragraph` must not reach it. See #1132.
func TestComputeSplit(t *testing.T) {
	info := Info{Lang: "en", Segmentation: true, Splitting: true}

	scopes := func(blks []Block) []string {
		found := []string{}
		for _, b := range blks {
			found = append(found, b.Scope)
		}
		return found
	}

	t.Run("a paragraph is split", func(t *testing.T) {
		blk := NewLinedBlock(
			"", "One sentence here. Two sentences here.", "text.md", 1)

		blks, err := info.Compute(&blk, true)
		if err != nil {
			t.Fatal(err)
		}

		want := []string{
			"paragraph.text.md",
			"sentence.text.md",
			"sentence.text.md",
			"text.md",
		}
		got := scopes(blks)
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("scopes = %v, want %v", got, want)
		}
	})

	t.Run("a heading is not", func(t *testing.T) {
		blk := NewLinedBlock(
			"", "A heading with a sentence.", "text.heading.h2.md", 1)

		blks, err := info.Compute(&blk, false)
		if err != nil {
			t.Fatal(err)
		}

		want := []string{
			"sentence.text.heading.h2.md",
			"text.heading.h2.md",
		}
		got := scopes(blks)
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("scopes = %v, want %v", got, want)
		}
	})

	t.Run("several paragraphs, several blocks", func(t *testing.T) {
		blk := NewLinedBlock(
			"", "First paragraph.\n\nSecond paragraph.", "text.md", 1)

		blks, err := info.Compute(&blk, true)
		if err != nil {
			t.Fatal(err)
		}

		count := 0
		for _, b := range blks {
			if strings.HasPrefix(b.Scope, "paragraph.") {
				count++
			}
		}
		if count != 2 {
			t.Errorf("paragraph blocks = %d, want 2", count)
		}
	})
}

// A block that inline markup has rewritten is nowhere in its context, so a
// match inside it can only be placed through the runs recorded as it was read.
// See #502.
func TestBlockSourceOffset(t *testing.T) {
	// "<p>a <b>b</b> c</p>" read as "a b c".
	runs := []Run{
		{At: 0, Src: 3, N: 2},  // "a "
		{At: 2, Src: 8, N: 1},  // "b"
		{At: 3, Src: 13, N: 2}, // " c"
	}

	tests := []struct {
		name  string
		blk   Block
		index int
		want  int
	}{
		{"first run", Block{Offset: -1, Runs: runs}, 0, 3},
		{"second run", Block{Offset: -1, Runs: runs}, 2, 8},
		{"third run", Block{Offset: -1, Runs: runs}, 4, 14},
		{"past the end", Block{Offset: -1, Runs: runs}, 5, -1},
		{"unmapped", Block{Offset: -1}, 0, -1},
		{"a placed block ignores runs", Block{Offset: 100, Runs: runs}, 2, 102},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.blk.SourceOffset(tt.index); got != tt.want {
				t.Errorf("SourceOffset(%d) = %d, want %d", tt.index, got, tt.want)
			}
		})
	}
}

// Segmenting a block hands its runs down to each sentence, rebased onto that
// sentence's own text -- otherwise only whole blocks could be placed.
func TestBlockWithRuns(t *testing.T) {
	// "One. <b>Two</b>." read as "One. Two." -- the sentence "Two." starts at 5.
	parent := []Run{
		{At: 0, Src: 0, N: 5}, // "One. "
		{At: 5, Src: 8, N: 4}, // "Two."
	}

	child := Block{Text: "Two.", Offset: -1}.withRuns(parent, 5)
	if got := child.SourceOffset(0); got != 8 {
		t.Errorf("SourceOffset(0) = %d, want 8", got)
	}

	// The first sentence keeps only the run it overlaps.
	first := Block{Text: "One. ", Offset: -1}.withRuns(parent, 0)
	if len(first.Runs) != 1 || first.Runs[0] != (Run{At: 0, Src: 0, N: 5}) {
		t.Errorf("Runs = %v, want one run covering %q", first.Runs, "One. ")
	}

	// An unplaced child inherits nothing rather than misplacing itself.
	lost := Block{Text: "Two.", Offset: -1}.withRuns(parent, -1)
	if lost.Runs != nil {
		t.Errorf("Runs = %v, want nil", lost.Runs)
	}
}

// Copies of a block share one summary; a hand-built block still has one.
func TestSummarizeIsSharedByCopies(t *testing.T) {
	blk := NewBlock("", "One sentence here. And another one.", "text")

	first := blk.Summarize()
	if first.NumSentences != 2 {
		t.Fatalf("NumSentences = %v, want 2", first.NumSentences)
	}

	copied := blk
	if copied.Summarize() != first {
		t.Error("a copy of the block summarized it again")
	}
	if blk.Summarize() != first {
		t.Error("a second call summarized the block again")
	}

	bare := Block{Text: "Just one."}
	if got := bare.Summarize().NumSentences; got != 1 {
		t.Errorf("hand-built block: NumSentences = %v, want 1", got)
	}
}

// A block's language chooses its segmenter: German knows "z.B." and "Dr."
// are abbreviations, and Japanese ends a sentence at 。 with no space after.
func TestComputeByLanguage(t *testing.T) {
	texts := func(blks []Block) []string {
		var out []string
		for _, b := range blks {
			if strings.HasPrefix(b.Scope, "sentence.") {
				out = append(out, b.Text)
			}
		}
		return out
	}

	cases := []struct {
		lang, text string
		want       []string
	}{
		{"de", "Das gilt z.B. für Dr. Müller. Er bleibt.", []string{"Das gilt z.B. für Dr. Müller.", "Er bleibt."}},
		{"ja", "東京に行きました。楽しかったです。", []string{"東京に行きました。", "楽しかったです。"}},
		{"", "Dr. Smith went home. He slept.", []string{"Dr. Smith went home.", "He slept."}},
	}
	for _, tc := range cases {
		info := Info{Lang: tc.lang, Segmentation: true}
		blk := NewLinedBlock("", tc.text, "text.md", 1)
		blks, err := info.Compute(&blk, false)
		if err != nil {
			t.Fatal(err)
		}
		got := texts(blks)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("[%s] sentences = %q, want %q", tc.lang, got, tc.want)
		}
	}
}

// Tagging follows the language too, with that language's tokenizer, and a
// language without a model is an error rather than English tags.
func TestTextToTokensByLanguage(t *testing.T) {
	toks, err := TextToTokens("L'homme mange une pomme rouge.", &Info{Lang: "fr"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tok := range toks {
		got = append(got, tok.Text+"/"+tok.Tag)
	}
	if want := "L'/DET homme/NOUN mange/VERB une/DET pomme/NOUN rouge/ADJ ./PUNCT"; strings.Join(got, " ") != want {
		t.Errorf("got %q, want %q", strings.Join(got, " "), want)
	}

	if _, jerr := TextToTokens("こんにちは。", &Info{Lang: "ja"}); jerr == nil {
		t.Error("Japanese has no tagging model; expected an error")
	}

	toks, err = TextToTokens("The dog runs.", nil)
	if err != nil || len(toks) != 4 || toks[2].Tag != "VBZ" {
		t.Errorf("English should still be Penn-tagged: %v (err=%v)", toks, err)
	}
}
