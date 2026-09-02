package nlp

import (
	"net/http"
	"net/http/httptest"
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

// Compute used to panic when a configured remote endpoint's /segment request
// failed -- a network error, a non-2xx status, a malformed response -- which
// crashed the whole vale process rather than reporting a normal lint error.
// Compute runs during block construction, ahead of any rule's own Run, so any
// sentence-scoped rule reaches this exact path just from being dispatched at
// all against a non-English document under a remote endpoint -- there is no
// rule-level error handling downstream to catch a panic here.
//
// A mocked /segment endpoint returning a non-2xx status confirms Compute now
// returns a normal error instead of panicking. Its caller, lintProse
// (internal/lint/lint.go), already wraps any error Compute returns as
// core.NewE100("NLP.Compute", err) -- that handling was already in place,
// only ever unreachable because Compute could not previously return an error
// on this path.
func TestComputeReturnsErrorOnSegmentEndpointFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"Sents":[]}`))
	}))
	defer server.Close()

	// A non-English language, so Compute reaches the remote branch rather
	// than local Punkt.
	info := Info{
		Segmentation: true,
		Lang:         "id",
		Endpoint:     server.URL,
	}
	blk := NewLinedBlock("", "I bought a widget. Arrived promptly.", "text.md", 1)

	var blks []Block
	var err error
	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Fatalf("Compute panicked instead of returning an error: %v", p)
			}
		}()
		blks, err = info.Compute(&blk, false)
	}()

	if err == nil {
		t.Fatalf("Compute returned a nil error for a failed /segment request, want a non-nil error")
	}
	if blks != nil {
		t.Errorf("blocks = %v, want nil alongside the error", blks)
	}
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
