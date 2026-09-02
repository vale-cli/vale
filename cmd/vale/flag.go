package main

import (
	"github.com/spf13/pflag"

	"github.com/vale-cli/vale/v3/internal/core"
)

// Flags are the user-defined CLI flags.
var Flags core.CLIFlags

var shortcodes = map[string]string{
	"version": "v",
	"help":    "h",
}

func init() {
	// Usage text is plain. These are built as the flags are registered, which
	// is before Vale knows where its output is going, so anything styled here
	// would still be styled when the help is piped into a file.
	pflag.StringVar(&Flags.Sources, "sources", "", "A list of config files to load.")
	pflag.StringVar(&Flags.Filter, "filter", "", "An expression to filter rules by.")
	pflag.StringVar(&Flags.Glob, "glob", "*", `A glob pattern (--glob='*.{md,txt}').`)
	pflag.StringVar(&Flags.Path, "config", "", `A file path (--config='some/file/path/.vale.ini').`)
	pflag.StringVar(&Flags.Output, "output", "CLI", `An output style ("line", "JSON", or a template file).`)
	pflag.StringVar(&Flags.InExt, "ext", ".txt", `An extension to associate with stdin (--ext=.md).`)
	pflag.StringVar(&Flags.InPath, "path", "", `A file path to associate with stdin (--path=docs/example.md).`)

	pflag.StringVar(&Flags.AlertLevel, "minAlertLevel", "",
		`The minimum level to display (--minAlertLevel=error).`)

	pflag.BoolVar(&Flags.NoColor, "no-color", false, "Don't colorize CLI output.")
	pflag.BoolVar(&Flags.PlainProgress, "plain-progress", false,
		"Log each step instead of drawing a progress bar.")
	pflag.BoolVar(&Flags.Wrap, "no-wrap", false, "Don't wrap CLI output.")
	pflag.BoolVar(&Flags.NoExit, "no-exit", false, "Don't return a nonzero exit code on errors.")
	pflag.BoolVar(&Flags.Counts, "counts", false,
		"Include per-check alert counts, zeros included, in JSON output.")
	pflag.BoolVar(&Flags.Apply, "apply", false,
		"With `vale fix`: write every unambiguous fix back to disk.")
	pflag.BoolVar(&Flags.Simple, "ignore-syntax", false, "Lint all files line-by-line.")
	pflag.BoolVarP(&Flags.Version, "version", "v", false, "Print the current version.")
	pflag.BoolVarP(&Flags.Help, "help", "h", false, "Print this help message.")

	pflag.BoolVar(&Flags.Local, "mode-compat", false, "Prioritize local Vale configurations.")
	pflag.BoolVar(&Flags.Sorted, "sort", false, "Sort files by their name in output.")
	pflag.BoolVar(&Flags.Normalize, "normalize", false, "Replace each path separator with a slash ('/').")
	pflag.BoolVar(&Flags.Relative, "relative", false, "Return relative paths.")
	pflag.BoolVar(&Flags.IgnoreGlobal, "no-global", false, "Don't load the global configuration.")
}
