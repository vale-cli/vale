package core

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jdkato/prose/v3/tag"
	_ "github.com/jdkato/prose/v3/tag/lang/fr" // a model to install under another name

	"github.com/vale-cli/vale/v3/internal/nlp"
)

func TestLanguages(t *testing.T) {
	cfg, err := NewConfig(&CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}
	cfg.FormatToLang = map[string]string{
		"*":      "DE",
		"*.md":   "pt_BR",
		"*.rst":  "en-US",
		"*.txt":  "",
		"*.adoc": "de",
	}

	if got, want := cfg.Languages(), []string{"de", "pt-br"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Languages() = %v, want %v", got, want)
	}
}

// A model directory under config/models is read through the configuration's
// filesystem and registered under the language that names it; a tag with a
// region reads its base language's directory.
func TestLoadLanguageModels(t *testing.T) {
	data, ok := tag.LanguageModel("fr")
	if !ok {
		t.Fatal("the French model should be linked into this test")
	}

	styles := t.TempDir()
	dir := filepath.Join(styles, ModelDir, "xx")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, taggerFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, abbreviationFile), []byte("bzw\nca\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := NewConfig(&CLIFlags{})
	if err != nil {
		t.Fatal(err)
	}
	cfg.AddStylesPath(styles)
	cfg.FormatToLang = map[string]string{"*": "xx-YY", "*.md": "zz"}

	if err = LoadLanguageModels(cfg); err != nil {
		t.Fatal(err)
	}

	tagger, err := nlp.TaggerFor("", "xx-YY")
	if err != nil {
		t.Fatal(err)
	}
	if got := tagger.(*tag.Tagger).Language(); got != "fr" {
		t.Errorf("the model under xx is the French one, got language %q", got)
	}

	// No directory: nothing registered, and the rule that needs it reports.
	if _, missing := nlp.TaggerFor("", "zz"); missing == nil {
		t.Error("a language with no model directory should not tag")
	}
}
