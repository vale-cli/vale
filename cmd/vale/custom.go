package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/Masterminds/sprig/v3"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/system"
)

// ProcessedFile represents a file that Vale has linted.
type ProcessedFile struct {
	Alerts []core.Alert
	Path   string
}

// Data holds the information exposed to UI templates.
type Data struct {
	Files       []ProcessedFile
	LintedTotal int
}

// PrintCustomAlerts formats the given alerts using a user-defined template.
func PrintCustomAlerts(linted []*core.File, cfg *core.Config) (bool, error) {
	var alertCount int

	path := cfg.Flags.Output
	if !system.FileExists(path) {
		path = core.FindAsset(cfg, path)
	}
	if path == "" {
		return false, core.NewE100("--output", fmt.Errorf(
			"'%s' isn't %s, or a template file that exists",
			cfg.Flags.Output, strings.Join(outputs, ", ")))
	}

	b, err := os.ReadFile(path)
	if err != nil {
		return false, core.NewE100("template", err)
	}
	text := string(b)

	t, err := template.New(filepath.Base(path)).Funcs(sprig.TxtFuncMap()).Funcs(funcs).Parse(text)
	if err != nil {
		return false, core.NewE100("template", err)
	}

	formatted := []ProcessedFile{}
	for _, f := range linted {
		if len(f.Alerts) == 0 {
			continue
		}
		for _, a := range f.SortedAlerts() {
			if a.Severity == "error" {
				alertCount++
				break
			}
		}
		formatted = append(formatted, ProcessedFile{
			Path:   f.Path,
			Alerts: f.Alerts,
		})
	}

	return alertCount != 0, t.Execute(os.Stdout, Data{
		Files:       formatted,
		LintedTotal: len(linted),
	})
}
