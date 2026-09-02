package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
)

// writeApplyFixture builds a config, three action-carrying rules, and the
// documents the tests rewrite.
func writeApplyFixture(t *testing.T, root string) {
	t.Helper()

	files := map[string]string{
		".vale.ini": "StylesPath = styles\nMinAlertLevel = suggestion\n\n[*]\nBasedOnStyles = T\n",
		"styles/T/Swap.yml": "extends: substitution\nmessage: \"Use '%s'.\"\nlevel: warning\n" +
			"action:\n  name: replace\nswap:\n  utilize: use\n  leverage: exploit\n",
		"styles/T/Multi.yml": "extends: existence\nmessage: \"Vague: '%s'.\"\nlevel: warning\n" +
			"action:\n  name: replace\n  params:\n    - alpha\n    - beta\ntokens:\n  - vagueterm\n",
		"styles/T/OverA.yml": "extends: substitution\nmessage: \"Use '%s'.\"\nlevel: warning\n" +
			"action:\n  name: replace\nswap:\n  quite bad: poor\n",
		"styles/T/OverB.yml": "extends: substitution\nmessage: \"Use '%s'.\"\nlevel: warning\n" +
			"action:\n  name: replace\nswap:\n  bad outcome: failure\n",
	}

	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), os.ModePerm); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// TestApplyFixes is the acceptance test for `fix --apply`: the two
// unambiguous swaps are written, the multi-suggestion alert and the
// overlapping pair are reported rather than guessed at, and a file with no
// action-carrying alerts comes back byte-identical.
func TestApplyFixes(t *testing.T) {
	root := t.TempDir()
	writeApplyFixture(t, root)

	doc := filepath.Join(root, "doc.md")
	body := "We should utilize the API and leverage the cache.\n\n" +
		"This vagueterm stays, and this was a quite bad outcome.\n"
	if err := os.WriteFile(doc, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	clean := filepath.Join(root, "clean.md")
	cleanBody := "Nothing actionable here at all.\n"
	if err := os.WriteFile(clean, []byte(cleanBody), 0o600); err != nil {
		t.Fatal(err)
	}
	cleanInfo, err := os.Stat(clean)
	if err != nil {
		t.Fatal(err)
	}

	flags := &core.CLIFlags{
		Path:  filepath.Join(root, ".vale.ini"),
		InExt: ".txt",
		Glob:  "*",
		Apply: true,
	}
	if err = applyFixes([]string{doc, clean}, flags); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(doc)
	if err != nil {
		t.Fatal(err)
	}
	want := "We should use the API and exploit the cache.\n\n" +
		"This vagueterm stays, and this was a quite bad outcome.\n"
	if string(got) != want {
		t.Errorf("applied content:\n%q\nwant:\n%q", got, want)
	}

	after, err := os.ReadFile(clean)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != cleanBody {
		t.Error("a file with no action-carrying alerts was rewritten")
	}
	if info, sErr := os.Stat(clean); sErr == nil &&
		info.ModTime() != cleanInfo.ModTime() {
		t.Error("a file with no action-carrying alerts was touched")
	}
}

// TestResolveEdits pins the conflict policy: exact duplicates collapse into
// one applied edit, and every member of an overlapping cluster is skipped.
func TestResolveEdits(t *testing.T) {
	report := fixedFile{}
	kept := resolveEdits([]fixEdit{
		{begin: 40, end: 50, text: "x", check: "T.C", line: 2, col: 1},
		{begin: 0, end: 5, text: "a", check: "T.A", line: 1, col: 1},
		{begin: 0, end: 5, text: "a", check: "T.B", line: 1, col: 1},
		{begin: 44, end: 60, text: "y", check: "T.D", line: 2, col: 5},
	}, &report)

	if len(kept) != 1 || kept[0].check != "T.A" {
		t.Errorf("kept = %v, want the deduplicated T.A edit alone", kept)
	}
	if len(report.Skipped) != 2 {
		t.Fatalf("skipped = %v, want the overlapping pair", report.Skipped)
	}
	if report.Skipped[0].Reason != "overlaps T.D" ||
		report.Skipped[1].Reason != "overlaps T.C" {
		t.Errorf("reasons = %v", report.Skipped)
	}
}

// TestByteSpan pins the line/column-to-byte mapping, including multi-byte
// characters and the match-equality guard.
func TestByteSpan(t *testing.T) {
	raw := []byte("naïve café here\nsecond line\n")
	starts := lineOffsets(raw)

	// "café" is characters 7-10 of line one.
	b, e, ok := byteSpan(raw, starts, core.Alert{
		Line: 1, Span: []int{7, 10}, Match: "café"})
	if !ok || string(raw[b:e]) != "café" {
		t.Errorf("byteSpan = %d, %d, %v", b, e, ok)
	}

	if _, _, ok = byteSpan(raw, starts, core.Alert{
		Line: 1, Span: []int{7, 10}, Match: "cafe"}); ok {
		t.Error("mapped a span whose bytes are not the match")
	}
	if _, _, ok = byteSpan(raw, starts, core.Alert{
		Line: 2, Span: []int{1, 99}, Match: "second"}); ok {
		t.Error("mapped a span past the end of its line")
	}
}
