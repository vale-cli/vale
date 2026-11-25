//go:build !vale_nocode

package code

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vale-cli/vale/v3/internal/lint/code/tsw"
)

// TestGrammarForExt tests that an extension with a grammar of its own gets
// it, and one without gets the grammar of the format it is normed to. See
// #1216: `.tsx` is normed to `.ts`, but only the TSX grammar has `jsx_text`.
func TestGrammarForExt(t *testing.T) {
	cases := []struct {
		ext   string
		query string
		ok    bool
	}{
		{".tsx", "(jsx_text) @prose", true},
		{".TSX", "(jsx_text) @prose", true},
		{".ts", "(jsx_text) @prose", false},
		{".mts", "(comment) @comment", true},
		{".jsx", "(jsx_text) @prose", true},
		{".cjs", "(jsx_text) @prose", true},
	}
	for _, c := range cases {
		lang, err := GetLanguageFromExt(c.ext)
		if err != nil {
			t.Fatalf("%s: %v", c.ext, err)
		}
		grammar, gErr := Grammar(lang.Grammar)
		if gErr != nil {
			t.Fatalf("%s: %v", c.ext, gErr)
		}
		tree, _ := grammar.Parse(nil)
		_, qErr := tree.Query(c.query)
		tree.Close()
		if ok := qErr == nil; ok != c.ok {
			t.Errorf("%s: query %q compiles = %v; want %v", c.ext, c.query, ok, c.ok)
		}
	}

	if _, err := GetLanguageFromExt(".nope"); err == nil {
		t.Fatal("expected an error for an unknown extension")
	}
}

// Files of one language linted at once load its grammar once: a second
// compile of the same module could hand a caller a module that isn't ready.
func TestGrammarLoadsOnce(t *testing.T) {
	if !tsw.Built {
		t.Skip("no grammar runtime in this build")
	}
	previous := Loader
	t.Cleanup(func() { Loader = previous })

	var calls atomic.Int32
	Loader = func(string) ([]byte, error) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond)             // long enough for every caller to arrive
		return []byte("\x00asm\x01\x00\x00\x00"), nil // an empty module
	}

	var wg sync.WaitGroup
	got := make([]*tsw.Language, 16)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g, err := Grammar("load-once")
			if err != nil {
				t.Error(err)
			}
			got[i] = g
		}()
	}
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Errorf("loaded %d times, want 1", n)
	}
	for _, g := range got {
		if g != got[0] {
			t.Fatal("callers got different grammars")
		}
	}

	// A failed load isn't kept: the next call tries again.
	Loader = func(name string) ([]byte, error) {
		calls.Add(1)
		return nil, &NotInstalledError{Name: name}
	}
	calls.Store(0)
	for range 2 {
		if _, err := Grammar("load-later"); err == nil {
			t.Fatal("want an error")
		}
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("tried %d times, want 2", n)
	}
}
