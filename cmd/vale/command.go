package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/jdkato/prose/v3/tag"
	"github.com/pterm/pterm"

	"github.com/vale-cli/vale/v3/internal/check"
	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/lint"
	"github.com/vale-cli/vale/v3/internal/nlp"
	"github.com/vale-cli/vale/v3/internal/system"
)

// TaggedWord is a word with an NLP context.
type TaggedWord struct {
	Token tag.Token
	Line  int
	Span  []int
}

// CompiledRule is a rule's compiled regex.
type CompiledRule struct {
	Pattern string
}

// A command is a subcommand of `vale` and everything its help says.
//
// Summary, Usage and Detail are one table rather than three so that a command
// cannot be listed without being documented, or documented without being
// reachable -- which is how `fix` came to be described in the help and hidden
// from it at the same time.
type command struct {
	// Run does the work.
	Run func(args []string, flags *core.CLIFlags) error

	// Summary is the single line the listing in `vale --help` shows.
	Summary string

	// Usage is the invocation, without the leading `vale`.
	Usage string

	// Detail is what `vale <name> --help` adds. Empty is fine for a command
	// whose summary says all there is to say.
	Detail string

	// Hidden keeps a command out of the listing and out of the suggestions,
	// without making it any less reachable for whoever knows the name.
	Hidden bool
}

// commands are the available CLI commands.
var commands = map[string]command{
	"ls-config": {
		Run:     printConfig,
		Summary: "Print the current configuration to stdout.",
		Usage:   "ls-config",
		Detail: "Resolves the configuration the way a lint run would -- every\n" +
			"source, in order -- and prints the result. This is the fastest way\n" +
			"to see which styles and rules a directory actually has in scope.",
	},
	"ls-metrics": {
		Run:     printMetrics,
		Summary: "Print the metrics of the given files to stdout.",
		Usage:   "ls-metrics <path>...",
		Detail: "Reports the counts Vale computed while reading each file --\n" +
			"words, sentences, paragraphs, and the elements of its markup.\n" +
			"One file prints its metrics alone; several, or a directory,\n" +
			"print an object keyed by file. --glob chooses a directory's files.",
	},
	"ls-dirs": {
		Run:     printDirs,
		Summary: "Print the default configuration directories to stdout.",
		Usage:   "ls-dirs",
		Detail:  "Shows where Vale looks for a StylesPath, a `.vale.ini`, and the\nnative messaging host, and whether each one is there.",
	},
	"ls-vars": {
		Run:     printVars,
		Summary: "Print the supported environment variables to stdout.",
		Usage:   "ls-vars",
		Detail:  "Lists every variable Vale reads, what it does, and its current value.",
	},
	"sync": {
		Run:     sync,
		Summary: "Download and install external configuration sources.",
		Usage:   "sync",
		Detail: "Reads the `Packages` key of the active configuration and installs\n" +
			"each entry into the StylesPath. Run this after adding a package,\n" +
			"and in CI before linting.",
	},
	"test": {
		Run:     runTests,
		Summary: "Run the test cases kept beside a configuration's rules.",
		Usage:   "test [path...]",
		Detail: "Cases live in a rule's `tests` list or in a `.test.yml` file. A\n" +
			"package's own `Packages` are installed for the run, so a rule that\n" +
			"extends another package resolves without `vale sync`.\n\n" +
			"With --coverage, every rule found under the given paths has to\n" +
			"produce an alert in at least one case. A rule that matches nothing\n" +
			"passes its cases silently, and this is what tells you.",
	},

	// Private: the API surface the editor integrations and the native host
	// use. Reachable by name, absent from the help.
	"host-install":   {Run: installNativeHost, Summary: "Install the native messaging host.", Hidden: true},
	"host-uninstall": {Run: uninstallNativeHost, Summary: "Uninstall the native messaging host.", Hidden: true},
	"compile":        {Run: compileRule, Summary: "Print a rule's compiled pattern.", Hidden: true},
	"run":            {Run: runRule, Summary: "Run a single rule against a file.", Hidden: true},
	"transform":      {Run: transform, Summary: "Print a file after its transform.", Hidden: true},
	"ls-path":        {Run: pathInfo, Summary: "Print the configuration files in scope.", Hidden: true},
	"fix":            {Run: fix, Summary: "Attempt to automatically fix the given alert.", Usage: "fix <alert> | fix --apply <path>...", Hidden: true},
	"tag":            {Run: runTag, Summary: "Print a file's part-of-speech tags.", Hidden: true},
	"dc":             {Run: printConfig, Summary: "Alias for `ls-config`.", Hidden: true},
	"load":           {Run: loadConfigs, Summary: "Print a merged pair of configurations.", Hidden: true},
}

func fix(args []string, flags *core.CLIFlags) error {
	if flags.Apply {
		return applyFixes(args, flags)
	}

	if len(args) != 1 {
		return core.NewE100("fix", errors.New("one argument expected"))
	}

	alert := args[0]
	if system.FileExists(args[0]) {
		b, err := os.ReadFile(args[0])
		if err != nil {
			return err
		}
		alert = string(b)
	}

	cfg, err := core.ReadPipeline(flags, false)
	if err != nil {
		return err
	}

	resp, err := check.ParseAlert(alert, cfg)
	if err != nil {
		return err
	}

	return printJSON(resp)
}

func sync(_ []string, flags *core.CLIFlags) error {
	cfg, err := core.ReadPipeline(flags, true)
	if err != nil {
		return err
	}

	// NOTE: sync should *only* run for a single config file.
	//
	// We resolve it before touching the `StylesPath` so that a missing config
	// reports itself rather than the empty path it leaves behind.
	rootINI, noRoot := cfg.Root()
	if noRoot != nil {
		return core.NewE100("sync", noRoot)
	} else if err = initPath(cfg); err != nil {
		return err
	}

	pkgs, err := core.GetPackages(rootINI)
	if err != nil {
		return err
	}
	stylesPath := cfg.StylesPath()

	// A progress bar is for someone watching it. Redirected into a CI log it
	// leaves a trail of redrawn frames and, at the end, no record of what was
	// actually installed -- so `--plain-progress` swaps it for a line per
	// package. See #1138.
	//
	// Asked for rather than inferred: a run that has been piped somewhere for
	// years should keep printing what it always printed.
	var bar *pterm.ProgressbarPrinter
	if !flags.PlainProgress {
		grammars, _ := grammarsToSync(cfg)
		bar, err = pterm.DefaultProgressbar.WithTotal(len(pkgs) + len(cfg.Languages()) + len(grammars)).Start()
		if err != nil {
			return err
		}
	}

	for idx, pkg := range pkgs {
		name := system.FileNameWithoutExt(pkg)

		if bar != nil {
			bar.UpdateTitle("Syncing " + name)
			bar.Increment()
		}

		if err = readPkg(pkg, stylesPath, idx); err != nil {
			return err
		}

		if bar == nil {
			fmt.Printf("Synced %s\n", name)
		}
	}

	// The models for the languages the configuration names, after the
	// packages: a package may be what names a language.
	synced := func(name, detail string) {
		switch {
		case bar != nil:
			bar.UpdateTitle("Syncing " + name)
			bar.Increment()
		case detail != "":
			fmt.Printf("Synced %s: %s\n", name, detail)
		default:
			fmt.Printf("Synced %s\n", name)
		}
	}

	nModels, err := syncModels(cfg, stylesPath, len(pkgs), synced)
	if err != nil {
		return err
	}

	nGrammars, err := syncGrammars(cfg, stylesPath, len(pkgs)+nModels, synced)
	if err != nil {
		return err
	}

	// A model makes a language's rules work; its spelling needs a
	// dictionary Vale doesn't carry, so say so here rather than at every
	// lint run.
	var undictionaried []string
	for _, lang := range cfg.Languages() {
		if dic, _ := core.DictionaryFor(cfg, lang); dic == "" {
			undictionaried = append(undictionaried, lang)
		}
	}

	summary := []string{fmt.Sprintf("%d package(s)", len(pkgs))}
	if nModels > 0 {
		summary = append(summary, fmt.Sprintf("%d language model(s)", nModels))
	}
	if nGrammars > 0 {
		summary = append(summary, fmt.Sprintf("%d grammar(s)", nGrammars))
	}
	pterm.Success.Printf("Synced %s to '%s'.\n", strings.Join(summary, ", "), stylesPath)
	if len(undictionaried) > 0 {
		pterm.Info.Printf("No dictionary for %s: Vale.Spelling skips those files until <name>.dic and <name>.aff are under %s.\n",
			strings.Join(undictionaried, ", "), filepath.Join(stylesPath, core.DictDir))
	}

	return nil
}

func printConfig(_ []string, flags *core.CLIFlags) error {
	cfg, err := core.ReadPipeline(flags, false)
	if err != nil {
		return err
	}
	fmt.Println(cfg.String())
	return nil
}

// printMetrics reports the metrics of each path given: `vale ls-metrics
// <path>...`. One file prints its metrics alone, as it always has; several
// paths, or a directory, print an object keyed by file, the way `--output=JSON`
// keys alerts. `--glob` chooses the files under a directory. See #1223.
func printMetrics(args []string, flags *core.CLIFlags) error {
	if len(args) == 0 {
		return core.NewE100("ls-metrics", errors.New("a file or directory expected"))
	}
	for _, arg := range args {
		if !system.FileExists(arg) {
			return core.NewE100("ls-metrics", fmt.Errorf("'%s' not found", arg))
		}
	}

	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		return err
	}

	cfg.MinAlertLevel = 0
	cfg.GBaseStyles = []string{"Vale"}
	cfg.Flags.InExt = ".txt" // default value
	cfg.Flags.Normalize = flags.Normalize

	linter, err := lint.NewLinter(cfg)
	if err != nil {
		return err
	}

	pat := "*"
	if flags.Glob != "" {
		pat = flags.Glob
	}
	linted, err := linter.Lint(args, pat)
	if err != nil {
		return err
	}

	// `FileExists` is true for a directory, and a directory that holds no
	// lintable file lints to nothing. Reporting that is better than printing
	// `{}`, which already means "a file Vale read and found no metrics in".
	if len(linted) == 0 {
		return core.NewE100("ls-metrics", fmt.Errorf(
			"'%s' contains no lintable files", strings.Join(args, "', '")))
	}

	if len(args) == 1 && !system.IsDir(args[0]) {
		return printJSON(roundedMetrics(linted[0]))
	}

	byFile := make(map[string]map[string]interface{}, len(linted))
	for _, f := range linted {
		byFile[f.Path] = roundedMetrics(f)
	}
	return printJSON(byFile)
}

// roundedMetrics is a file's metrics with each score at two places, which is
// what a rule's message shows; a score otherwise carries the digits of its
// division.
func roundedMetrics(f *core.File) map[string]interface{} {
	computed, _ := f.ComputeMetrics()
	for k, v := range computed {
		if n, ok := v.(float64); ok && n != math.Trunc(n) {
			computed[k] = math.Round(n*100) / 100
		}
	}
	return computed
}

// runTag tags a file with the language's model: `vale tag <file> <lang>`.
func runTag(args []string, flags *core.CLIFlags) error {
	if len(args) != 2 {
		return core.NewE100("tag", errors.New("a file and a language expected"))
	}

	text, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}

	info := nlp.Info{Lang: args[1]}

	// Another language's model is in the StylesPath, which only the
	// configuration knows; English needs none.
	if !nlp.IsEnglish(info.Lang) {
		cfg, cerr := core.ReadPipeline(flags, false)
		if cerr != nil {
			return cerr
		}
		if cerr = core.LoadLanguageModels(cfg); cerr != nil {
			return cerr
		}
	}

	out, err := core.TextToContext(string(text), &info)
	if err != nil {
		return core.NewE100("tag", err)
	}

	return printJSON(out)
}

func compileRule(args []string, _ *core.CLIFlags) error {
	if len(args) != 1 {
		return core.NewE100("compile", errors.New("one argument expected"))
	}

	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		return err
	}

	path := args[0]
	name := filepath.Base(path)

	mgr, err := check.NewManager(cfg)
	if err != nil {
		return err
	}

	err = mgr.AddRuleFromFile(name, path)
	if err != nil {
		return err
	}

	rule := CompiledRule{Pattern: mgr.Rules()[name].Pattern()}
	return printJSON(rule)
}

func runRule(args []string, _ *core.CLIFlags) error {
	if len(args) != 2 {
		return core.NewE100("run", errors.New("two arguments expected"))
	}

	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		return err
	}

	cfg.MinAlertLevel = 0
	cfg.GBaseStyles = []string{"Test"}
	cfg.Flags.InExt = ".txt" // default value

	linter, err := lint.NewLinter(cfg)
	if err != nil {
		return err
	}

	err = linter.Manager.AddRuleFromFile("Test.Rule", args[0])
	if err != nil {
		return err
	}

	linted, err := linter.Lint([]string{args[1]}, "*")
	if err != nil {
		return err
	}

	PrintJSONAlerts(linted)
	return nil
}

func printVars(_ []string, flags *core.CLIFlags) error {
	if flags.Output == "JSON" {
		var out = map[string]string{}
		for name := range core.ConfigVars {
			value, _ := os.LookupEnv(name)
			out[name] = value
		}
		return printJSON(out)
	}

	tableData := pterm.TableData{
		{"Variable", "Description", "Value"},
	}

	for name, info := range core.ConfigVars {
		found := pterm.FgRed.Sprint("✗")
		if value, ok := os.LookupEnv(name); ok {
			found = value
		}
		tableData = append(tableData, []string{toCodeStyle(name), info, found})
	}

	return pterm.DefaultTable.WithHasHeader().WithData(tableData).Render()
}

func printDirs(_ []string, flags *core.CLIFlags) error {
	styles, _ := core.DefaultStylesPath()

	stylesFound := pterm.FgGreen.Sprint("✓")
	if !system.IsDir(styles) {
		stylesFound = pterm.FgRed.Sprint("✗")
	}

	cfg, _ := core.DefaultConfig()

	configFound := pterm.FgGreen.Sprint("✓")
	if !system.FileExists(cfg) {
		configFound = pterm.FgRed.Sprint("✗")
	}

	native, _ := getNativeConfig()
	nativeDir := filepath.Dir(native)
	nativeExe := filepath.Join(nativeDir, getExecName("vale-native"))

	nativeFound := pterm.FgGreen.Sprint("✓")
	if !system.FileExists(native) {
		nativeFound = pterm.FgRed.Sprint("✗")
	}

	if flags.Output == "JSON" {
		return printJSON(map[string]string{
			"StylesPath":  styles,
			".vale.ini":   cfg,
			"vale-native": nativeExe,
		})
	}

	tableData := pterm.TableData{
		{"Asset", "Default Location", "Found"},
		{toCodeStyle("StylesPath"), styles, stylesFound},
		{toCodeStyle(".vale.ini"), cfg, configFound},
		{toCodeStyle("vale-native"), nativeExe, nativeFound},
	}

	return pterm.DefaultTable.WithHasHeader().WithData(tableData).Render()
}

func transform(args []string, flags *core.CLIFlags) error {
	if len(args) != 1 {
		return core.NewE100("transform", errors.New("one argument expected"))
	} else if !system.FileExists(args[0]) {
		return fmt.Errorf("file not found: %s", args[0])
	}

	cfg, err := core.ReadPipeline(flags, false)
	if err != nil {
		return err
	}

	linter, err := lint.NewLinter(cfg)
	if err != nil {
		return err
	}

	f, err := core.NewFile(args[0], cfg)
	if err != nil {
		return err
	}

	out, err := linter.Transform(f)
	if err != nil {
		return err
	}

	fmt.Println(out)
	return nil
}

func pathInfo(_ []string, flags *core.CLIFlags) error {
	system := map[string][]string{}

	cfg, err := core.ReadPipeline(flags, false)
	if err != nil {
		return err
	}

	system["configs"] = append(system["configs"], cfg.ConfigFiles...)
	return printJSON(system)
}

func loadConfigs(args []string, _ *core.CLIFlags) error {
	if len(args) != 2 {
		return core.NewE100("run", errors.New("two arguments expected"))
	}

	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		return err
	}
	cfg.RootINI = args[0]

	cfg.AddConfigFile(args[0])
	cfg.AddConfigFile(args[1])

	err = core.MockLoad(args[0], args[1], cfg)
	if err != nil {
		return err
	}

	return printJSON(cfg)
}
