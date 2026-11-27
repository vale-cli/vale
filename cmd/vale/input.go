package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/vale-cli/vale/v3/internal/core"
)

// outputs are the built-in values of --output, as the rest of Vale spells
// them.
var outputs = []string{"CLI", "line", "JSON"}

// checkFlags settles the flags whose values Vale reads by exact spelling,
// and refuses a value that would otherwise be ignored without a word.
func checkFlags(flags *core.CLIFlags) error {
	for _, name := range outputs {
		if strings.EqualFold(flags.Output, name) {
			flags.Output = name
		}
	}

	// `--ext=md` is `--ext=.md`: without the dot it matched no section.
	if flags.InExt != "" && !strings.HasPrefix(flags.InExt, ".") {
		flags.InExt = "." + flags.InExt
	}

	if flags.AlertLevel != "" && !core.StringInSlice(flags.AlertLevel, core.AlertLevels) {
		return core.NewE100("--minAlertLevel", fmt.Errorf(
			"'%s' isn't a level; the levels are %s",
			flags.AlertLevel, strings.Join(core.AlertLevels, ", ")))
	}
	return nil
}

// reExt matches a file extension: `.md`, `.adoc`, `.c`, but not the end of
// a sentence or of a version number.
var reExt = regexp.MustCompile(`^\.[A-Za-z0-9]*[A-Za-z][A-Za-z0-9]*$`)

// looksLikePath reports whether an argument that names nothing on disk was
// meant as a path rather than as text to lint: one word, with a directory
// separator or a file extension. Linted as text, a mistyped path reported
// no alerts and exited 0.
func looksLikePath(s string) bool {
	if strings.ContainsAny(s, " \t\n") {
		return false
	}
	return strings.ContainsAny(s, `/\`) || reExt.MatchString(filepath.Ext(s))
}
