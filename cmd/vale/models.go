package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"

	"github.com/jdkato/prose/v3/models"
	"github.com/pterm/pterm"

	"github.com/vale-cli/vale/v3/internal/core"
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
