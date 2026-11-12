package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/jdkato/prose/v3/models"
	"github.com/pterm/pterm"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/lint/code"
	"github.com/vale-cli/vale/v3/internal/system"
)

// A language's models come from the prose release that matches the prose
// this binary was built with, so the archive is always in a format it reads.
const (
	proseModule   = "github.com/jdkato/prose/v3"
	proseReleases = "https://github.com/jdkato/prose/releases/download/models-%s/%s"
)

// modelsURLVar overrides where models come from: a URL, or a directory
// holding one archive per language -- for a mirror, an air-gapped machine,
// or a test.
const modelsURLVar = "VALE_MODELS_URL"

// syncModels installs the model archive for every language the
// configuration names, after the packages; idx continues their numbering.
//
// A language prose publishes no model for is a warning, not an error: it may
// be one Vale segments by rules alone, as Japanese is.
func syncModels(cfg *core.Config, stylesPath string, idx int, report func(name string)) (int, error) {
	synced := 0
	for i, lang := range cfg.Languages() {
		if _, ok := models.Lookup(lang); !ok {
			pterm.Warning.Printf("No model is published for language '%s'; its text is segmented by punctuation, and rules that need tags will report the missing model.\n", lang)
			continue
		}

		src, err := modelSource(lang)
		if err != nil {
			return synced, core.NewE100("sync", err)
		}
		if err = loadPkg(lang, src, stylesPath, idx+i); err != nil {
			return synced, err
		}
		report(lang)
		synced++
	}
	return synced, nil
}

// modelSource is where lang's archive is: under VALE_MODELS_URL when set,
// else in the release matching the linked prose.
func modelSource(lang string) (string, error) {
	archive := models.Archive(lang)

	if base := os.Getenv(modelsURLVar); base != "" {
		if strings.Contains(base, "://") {
			return strings.TrimSuffix(base, "/") + "/" + archive, nil
		}
		// A directory of archives, or of unpacked packages.
		if dir := filepath.Join(base, lang); system.IsDir(dir) {
			return dir, nil
		}
		return filepath.Join(base, archive), nil
	}

	release, ok := proseVersion()
	if !ok {
		return "", fmt.Errorf("this build of Vale has no release of prose to fetch models from; set %s", modelsURLVar)
	}
	return fmt.Sprintf(proseReleases, release, archive), nil
}

// rePseudo matches a pseudo-version, which names a commit rather than a
// release: v3.2.3-0.20261106120000-abcdef123456, with or without a
// pre-release label before the timestamp.
var rePseudo = regexp.MustCompile(`\d{14}-[0-9a-f]{12}$`)

// isRelease reports whether a module version is a tagged release.
func isRelease(version string) bool {
	return version != "" && !rePseudo.MatchString(version) && !strings.HasSuffix(version, "+incompatible")
}

// proseVersion is the version of prose in this binary, when it is a release
// rather than a development snapshot.
func proseVersion() (string, bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	for _, dep := range info.Deps {
		if dep.Path != proseModule {
			continue
		}
		if dep.Replace != nil {
			dep = dep.Replace
		}
		if !isRelease(dep.Version) {
			return "", false
		}
		return dep.Version, true
	}
	return "", false
}

// A grammar comes from the Vale release this binary is, as
// grammar-<name>.zip; grammars/pack.sh writes the archives and the release
// workflow attaches them.
const grammarReleases = "https://github.com/vale-cli/vale/releases/download/%s/%s"

// grammarsURLVar overrides where grammars come from, as modelsURLVar does
// for models.
const grammarsURLVar = "VALE_GRAMMARS_URL"

// grammarsToSync is the grammars a configuration asks for with its
// `Grammars` key: the ones it names, or every one for `ALL`. Without the
// key, the ones its sections imply: a `[*.go]` section is going to lint Go,
// so it gets the Go grammar without naming it. An unknown name is an error,
// since nothing would be found for it.
func grammarsToSync(cfg *core.Config) ([]string, error) {
	if cfg.Grammars == nil {
		return impliedGrammars(cfg), nil
	}
	known := map[string]bool{}
	for _, name := range code.Names() {
		known[name] = true
	}
	var out []string
	for _, name := range cfg.Grammars {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "all" {
			return code.Names(), nil
		}
		if !known[name] {
			return nil, fmt.Errorf("no grammar named %q; the grammars are %s", name, strings.Join(code.Names(), ", "))
		}
		out = append(out, name)
	}
	return out, nil
}

// impliedGrammars is the grammars of the code extensions the configuration's
// sections name: `*.go`, `*.{py,rs}`, `src/**/*.ts`. Named, not matched: a
// `[*]` section matches every file, but asking for it is not asking for
// every grammar, which is what `Grammars = ALL` is for.
func impliedGrammars(cfg *core.Config) []string {
	seen := map[string]bool{}
	for _, pat := range cfg.SecToPat {
		if pat.Negated {
			continue
		}
		for _, ext := range code.Extensions() {
			if !namesExt(pat.Pattern, ext) {
				continue
			}
			if lang, err := code.GetLanguageFromExt(ext); err == nil && lang.Grammar != "" {
				seen[lang.Grammar] = true
			}
		}
	}

	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// namesExt reports whether a glob spells out an extension: `go` in `*.go`
// or `*.{go,py}`, as a whole word, so `*.golden` is not Go.
func namesExt(pattern, ext string) bool {
	word := ext[1:]
	for at := 0; ; {
		i := strings.Index(pattern[at:], word)
		if i < 0 {
			return false
		}
		i += at
		end := i + len(word)
		if (i == 0 || !isWordByte(pattern[i-1])) && (end == len(pattern) || !isWordByte(pattern[end])) {
			return true
		}
		at = i + 1
	}
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// syncGrammars installs the grammars the configuration asks for, after the
// packages and models; idx continues their numbering.
func syncGrammars(cfg *core.Config, stylesPath string, idx int, report func(name string)) (int, error) {
	names, err := grammarsToSync(cfg)
	if err != nil {
		return 0, core.NewE100("sync", err)
	}
	for i, name := range names {
		pkg := "grammar-" + name
		src, serr := grammarSource(pkg)
		if serr != nil {
			return i, core.NewE100("sync", serr)
		}
		if lerr := loadPkg(pkg, src, stylesPath, idx+i); lerr != nil {
			return i, lerr
		}
		report(pkg)
	}
	return len(names), nil
}

// grammarSource is where a grammar package is: under VALE_GRAMMARS_URL when
// set, else in this Vale's own release.
func grammarSource(pkg string) (string, error) {
	archive := pkg + ".zip"

	if base := os.Getenv(grammarsURLVar); base != "" {
		if strings.Contains(base, "://") {
			return strings.TrimSuffix(base, "/") + "/" + archive, nil
		}
		if dir := filepath.Join(base, pkg); system.IsDir(dir) {
			return dir, nil
		}
		return filepath.Join(base, archive), nil
	}

	if version == "master" || !isRelease(version) {
		return "", fmt.Errorf("this build of Vale is not a release to fetch grammars from; set %s", grammarsURLVar)
	}
	return fmt.Sprintf(grammarReleases, "v"+strings.TrimPrefix(version, "v"), archive), nil
}
