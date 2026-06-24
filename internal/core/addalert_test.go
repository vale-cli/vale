package core

import (
	"testing"

	"github.com/errata-ai/vale/v3/internal/nlp"
)

// AddAlert must not panic on an alert with a negative Span -- spelling can
// produce one when the matched token isn't found verbatim in the block. See
// #808 (panic: slice bounds out of range [-1:]).
func TestAddAlertNegativeSpan(t *testing.T) {
	f := &File{
		ChkToCtx: map[string]string{},
		history:  map[string]int{},
		limits:   map[string]int{},
	}
	// count("word") > 1 and ctx < 1000 -> the disambiguation branch that
	// previously sliced ctx[0:Span[0]] with a negative index.
	blk := nlp.NewBlock("word and word", "word and word", "text.md")
	a := Alert{Check: "X", Match: "word", Span: []int{-1, 3}}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("AddAlert panicked on a negative span: %v", r)
		}
	}()
	f.AddAlert(a, blk, 1, 0, false)
}
