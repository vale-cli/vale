//go:build !vale_noexpr

package core

import (
	"path/filepath"
	"testing"
)

func TestSplitSection(t *testing.T) {
	cases := []struct{ label, glob, cond string }{
		{"*.md", "*.md", ""},
		{"*.md if .Meta.draft", "*.md", ".Meta.draft"},
		{"docs/*.md if .Name == \"a if b\"", "docs/*.md", ".Name == \"a if b\""},
		{"*.{md,txt}", "*.{md,txt}", ""},
	}
	for _, c := range cases {
		if g, cond := SplitSection(c.label); g != c.glob || cond != c.cond {
			t.Errorf("SplitSection(%q) = %q, %q; want %q, %q", c.label, g, cond, c.glob, c.cond)
		}
	}
}

func TestCondition(t *testing.T) {
	root := t.TempDir()
	cfg, err := NewConfig(&CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}
	cfg.RootINI = filepath.Join(root, ".vale.ini")

	yaml := "---\npublished: false\ntags: [a, b]\n---\n\nText.\n"
	toml := "+++\ndraft = true\n+++\n\nText.\n"

	cases := []struct {
		expr, path, content string
		want                bool
	}{
		{`.Meta.published == false`, "post.md", yaml, true},
		{`.Meta.published == false`, "post.md", "Text.\n", false},
		{`.Meta.draft`, "post.md", toml, true},
		{`.Meta.draft`, "post.md", yaml, false},
		{`.Meta.published == false or .Meta.draft`, "post.md", "Text.\n", false},
		{`.Meta.draft or .Meta.published == false`, "post.md", yaml, true},
		{`not .Meta.draft and .Ext == ".md"`, "post.md", yaml, true},
		{`"b" in .Meta.tags`, "post.md", yaml, true},
		{`"b" in .Meta.tags`, "post.md", "Text.\n", false},
		{`.Name == "post.md" and .Ext == ".md"`, "post.md", "", true},
		{`.Path == "docs/a/post.md" and .Dir == "docs/a"`, filepath.Join(root, "docs", "a", "post.md"), "", true},
	}
	for _, c := range cases {
		cond, cErr := NewCondition(c.expr)
		if cErr != nil {
			t.Fatalf("NewCondition(%q): %v", c.expr, cErr)
		}
		got, hErr := cond.Holds(NewFileFields(c.path, c.content, cond.meta, cfg))
		if hErr != nil {
			t.Fatalf("%q on %s: %v", c.expr, c.path, hErr)
		}
		if got != c.want {
			t.Errorf("%q on %s = %v, want %v", c.expr, c.path, got, c.want)
		}
	}
}

func TestConditionErrors(t *testing.T) {
	for _, bad := range []string{`.Meta.published ==`, `"text"`} {
		if _, err := NewCondition(bad); err == nil {
			t.Errorf("NewCondition(%q) compiled; want an error", bad)
		}
	}
}

// TestRelToRoot tests that `.Path` is relative to the project root: the
// directory the search found the config at when there is one, otherwise the
// config file's own directory.
func TestRelToRoot(t *testing.T) {
	root := t.TempDir()
	doc := filepath.Join(root, "docs", "a.md")

	cfg := &Config{RootINI: filepath.Join(root, ".config", "vale.ini"), RootDir: root}
	if got := filepath.ToSlash(relToRoot(doc, cfg)); got != "docs/a.md" {
		t.Fatalf("with RootDir: got %q", got)
	}

	cfg = &Config{RootINI: filepath.Join(root, ".vale.ini")}
	if got := filepath.ToSlash(relToRoot(doc, cfg)); got != "docs/a.md" {
		t.Fatalf("from RootINI: got %q", got)
	}
}

// TestSectionAppliesSeparators tests that a section glob written with forward
// slashes matches a path written with the OS separator.
func TestSectionAppliesSeparators(t *testing.T) {
	cfg, err := NewConfig(&CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join("docs", "guide", "a.md")
	if !cfg.SectionApplies("docs/**/*.md", nil, path) {
		t.Fatalf("[docs/**/*.md] should match %q", path)
	}
	if cfg.SectionApplies("src/*.md", nil, path) {
		t.Fatalf("[src/*.md] should not match %q", path)
	}
}
