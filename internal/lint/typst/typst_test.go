//go:build !vale_nocode

package typst

import (
	"strings"
	"sync"
	"testing"
)

func TestConvert(t *testing.T) {
	got, err := Convert("= A naïve heading\n\nSome *strong* prose and `raw`.\n\n#let x = [kept] + 1\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<h1>A naïve heading</h1>", "<strong>strong</strong>", "<code>raw</code>", "kept",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "let") {
		t.Errorf("code mode was not dropped:\n%s", got)
	}
}

func TestConvertEmptyAndMalformed(t *testing.T) {
	for _, doc := range []string{"", "#let x = (", "*unclosed", "\xff\xfe"} {
		if _, err := Convert(doc); err != nil {
			t.Errorf("%q: %v", doc, err)
		}
	}
}

// Files are linted concurrently, and an instance is single-threaded.
func TestConvertConcurrently(t *testing.T) {
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			doc := strings.Repeat("= Heading\n\nProse here.\n\n", i+1)
			got, err := Convert(doc)
			if err != nil {
				t.Error(err)
			} else if n := strings.Count(got, "<h1>Heading</h1>"); n != i+1 {
				t.Errorf("want %d headings, got %d", i+1, n)
			}
		}()
	}
	wg.Wait()
}
