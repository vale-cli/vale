package code

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestAppendLineNormalizesTrailingNewlines(t *testing.T) {
	tests := map[string]string{
		"":           "\n",
		"text":       "text\n",
		"text\n":     "text\n",
		"text\n\n":   "text\n",
		"  text\n\n": "text\n",
	}

	for input, expected := range tests {
		if actual := appendLine(input); actual != expected {
			t.Errorf("appendLine(%q) = %q, want %q", input, actual, expected)
		}
	}
}

var testDir = "../../../testdata/comments"
var binDir = "../../../bin"

func TestComments(t *testing.T) {
	var cleaned []fs.DirEntry

	cases, err := os.ReadDir(testDir + "/in")
	if err != nil {
		t.Error(err)
	}

	for _, f := range cases {
		if f.Name() == ".DS_Store" {
			continue
		}
		cleaned = append(cleaned, f)
	}

	for i, f := range cleaned {
		b, err1 := os.ReadFile(fmt.Sprintf("%s/in/%s", testDir, f.Name()))
		if err1 != nil {
			t.Error(err1)
		}

		lang, err2 := GetLanguageFromExt(filepath.Ext(f.Name()))
		if err2 != nil {
			t.Error(err2)
		}

		comments, err3 := GetComments(b, lang)
		if err3 != nil {
			t.Error(err3)
		}

		b2, err4 := os.ReadFile(fmt.Sprintf("%s/out/%d.json", testDir, i))
		if err4 != nil {
			t.Error(err4)
		}

		markup := toJSON(comments)
		if markup != string(b2) {
			bin := filepath.Join(binDir, fmt.Sprintf("%d.json", i))
			_ = os.WriteFile(bin, []byte(markup), 0600)
			t.Errorf("%s", markup)
		}
	}
}

// TestPredicateOnlyCapture covers the underscore convention: a capture that
// exists for a predicate to test is not itself linted.
//
// Without it, testing one node and extracting another is impossible -- the
// only testable node is the one you extract -- which is what the Elixir doc
// attributes need (the attribute name decides, the heredoc is the prose).
func TestPredicateOnlyCapture(t *testing.T) {
	source := []byte("defmodule M do\n  @moduledoc \"Prose.\"\nend\n")

	lang, err := GetLanguageFromExt(".ex")
	if err != nil {
		t.Fatal(err)
	}

	comments, err := GetComments(source, lang)
	if err != nil {
		t.Fatal(err)
	}

	if len(comments) != 1 {
		t.Fatalf("got %d comments, want 1: %v", len(comments), comments)
	} else if comments[0].Text != "Prose." {
		t.Errorf("got %q, want %q", comments[0].Text, "Prose.")
	}
}

// TestDocAttributesWithoutProse covers the attributes that hold no prose:
// `@doc false` hides a function and `@doc since:` is metadata. Neither has a
// string argument, so neither is extracted.
func TestDocAttributesWithoutProse(t *testing.T) {
	source := []byte("defmodule M do\n  @doc false\n  @doc since: \"1.0.0\"\n  def f, do: :ok\nend\n")

	lang, err := GetLanguageFromExt(".ex")
	if err != nil {
		t.Fatal(err)
	}

	comments, err := GetComments(source, lang)
	if err != nil {
		t.Fatal(err)
	}

	if len(comments) != 0 {
		t.Errorf("got %d comments, want 0: %v", len(comments), comments)
	}
}
