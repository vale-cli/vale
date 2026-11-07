package code

import "testing"

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
