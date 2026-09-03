package check

import (
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

// A plain (sentence-scoped) rule's Run call against a block Info.Compute
// already built as one segmenter piece -- the common case, and the one this
// PR's optimization targets. Local English tagging never touches a remote
// endpoint either way, so this isolates whatever local cost the skipped
// f.Sentences call itself carried (a map lookup into TokenCache.sentences
// after the first call, since the cache is keyed by exact text and this
// benchmark reuses the same block every iteration).
func BenchmarkSequenceRunPlainRuleSentenceBlock(b *testing.B) {
	rule, err := NewSequence(testConfig(), baseCheck{
		"extends":    "sequence",
		"name":       "Bench.WidgetArrived",
		"level":      "error",
		"ignorecase": true,
		"message":    "matched",
		"tokens": []interface{}{
			map[string]interface{}{"pattern": "widget"},
			map[string]interface{}{"pattern": "arrived", "skip": 1},
		},
	}, "Bench.WidgetArrived")
	if err != nil {
		b.Fatalf("building rule: %v", err)
	}

	text := "I bought a widget that arrived promptly, said the courier."
	f := &core.File{NLP: nlp.Info{Segmentation: true}}
	blk := nlp.NewLinedBlock("", text, "sentence.text.md", 1)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, rerr := rule.Run(blk, f, testConfig()); rerr != nil {
			b.Fatalf("running rule: %v", rerr)
		}
	}
}
