package lint

import (
	"strings"
	"testing"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

// Both readers of a source file consult skipsComment, so a scope excluded from
// one is excluded from the other. Mapping a markup format onto a file switches
// which reader runs, and used to switch this off with it. See #858.
func TestSkipsComment(t *testing.T) {
	tests := []struct {
		name    string
		ignored []string
		scope   string
		want    bool
	}{
		{"nothing ignored", nil, "text.comment.block", false},
		{"the scope itself", []string{"text.comment.block"}, "text.comment.block", true},
		{"every comment", []string{"comment"}, "text.comment.block", true},
		{"a different scope", []string{"text.comment.line"}, "text.comment.block", false},
		{"alongside the defaults", []string{"code", "tt", "text.comment.block"},
			"text.comment.block", true},
	}

	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}
	linter, err := NewLinter(cfg)
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			linter.Manager.Config.IgnoredScopes = tt.ignored
			if got := linter.skipsComment(tt.scope); got != tt.want {
				t.Errorf("skipsComment(%q) = %v with %v, want %v",
					tt.scope, got, tt.ignored, tt.want)
			}
		})
	}
}

// The format a file is read as supplies the grammar for an extension that has
// none of its own; without one, such a file is linted by the regex fallback,
// which has no JavaScript entry and so reads no comments at all.
func TestLanguageForMappedExtension(t *testing.T) {
	tests := []struct {
		name    string
		formats map[string]string
		ext     string
		want    bool
	}{
		{"own grammar", nil, ".js", true},
		{"unmapped, no grammar", nil, ".es", false},
		{"mapped to a grammar", map[string]string{"es": "js"}, ".es", true},
		{"mapped to a format without one", map[string]string{"es": "md"}, ".es", false},
		{"mapped by a glob", map[string]string{"*.es": "js"}, ".es", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normed, _ := core.FormatFromExt("test"+tt.ext, tt.formats)
			lang, langErr := (&Linter{}).languageFor(&core.File{RealExt: tt.ext, NormedExt: normed})
			if got := langErr == nil && lang != nil; got != tt.want {
				t.Errorf("languageFor(%q) with %v found a grammar = %v, want %v",
					tt.ext, tt.formats, got, tt.want)
			}
		})
	}
}

// A comment is prose, so it is segmented like a text file: a rule scoped to
// `sentence` or `paragraph` gets a block of its own to run on. A comment
// found by its delimiters is located by search, so its parts carry no offset;
// one a grammar found is its own context, and its parts are placed in it.
func TestCommentIsSegmented(t *testing.T) {
	text := "// One sentence. Then another one.\n"

	tests := []struct {
		name   string
		block  nlp.Block
		placed bool
	}{
		{"by delimiters", nlp.NewBlock(text, text, "text.comment.line.rs"), false},
		{"by grammar", nlp.NewBlock("", text, "text.comment.line.rs"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			linter := demoLinter(t)

			f, err := core.NewFileFromContent(text, "main.rs", linter.Manager.Config)
			if err != nil {
				t.Fatal(err)
			}
			f.NLP = nlp.Info{Lang: "en", Segmentation: true, Splitting: true}

			seen := map[string]int{}
			linter.BlockHook = func(b nlp.Block) {
				kind, _, _ := strings.Cut(b.Scope, ".")
				seen[kind]++
				if placed := b.Offset >= 0; placed != tt.placed {
					t.Errorf("%s block %q: placed = %v, want %v", kind, b.Text, placed, tt.placed)
				}
			}

			if err = linter.lintCommentBlock(f, tt.block, 1, 0); err != nil {
				t.Fatal(err)
			}

			want := map[string]int{"sentence": 2, "paragraph": 1, "text": 1}
			for kind, n := range want {
				if seen[kind] != n {
					t.Errorf("got %d %s blocks, want %d", seen[kind], kind, n)
				}
			}
		})
	}
}
