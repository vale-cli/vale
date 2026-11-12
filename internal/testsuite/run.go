package testsuite

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/lint"
	"github.com/vale-cli/vale/v3/internal/nlp"
	"github.com/vale-cli/vale/v3/internal/system"
)

// A Result is what running one Case produced.
type Result struct {
	Case Case

	// Got is the alert output, one `line:col:check:message` per line. There is
	// no path: the document was a string.
	Got string

	// Reason says what went wrong, and is empty when the case passed. Err is
	// set instead when the case could not be run at all -- a rule that does
	// not compile is not a failing assertion.
	Reason string
	Err    error

	// Tags is Input as the tagger read it, `word/TAG` per token, for a
	// failing case of a `sequence` rule: the tags are what the rule matched
	// against, and they are invisible in the YAML.
	Tags string
}

// Failed reports whether the case did not pass, for either reason.
func (r Result) Failed() bool { return r.Reason != "" || r.Err != nil }

// A Runner lints the cases in a configuration.
type Runner struct {
	flags *core.CLIFlags

	// project is the linter a `vale` run in this directory would use. It is
	// built once and reused: loading a configuration costs more than linting
	// the handful of lines a case holds.
	project *lint.Linter

	// paths is the configuration's StylesPath search list, read once and only
	// for naming isolated rules. Empty when there is no configuration, which
	// is fine: isolation never needed one before and still doesn't.
	paths    []string
	pathsSet bool

	// fs is the filesystem cases read rules and documents through, so a
	// configuration over in-memory files tests exactly the files it holds.
	fs system.FS

	// cfg is a configuration handed in already resolved, by NewRunnerFor;
	// nil means the one `flags` finds on disk.
	cfg *core.Config

	// Install puts packages into a StylesPath the way `vale sync` does. With
	// it, a package's own `Packages` are installed for its isolated cases, so
	// a rule that extends Std resolves without a synced project.
	Install func(pkgs []string, styles string) error

	// deps holds the extra search paths found for each package root, and
	// temp the directories Install wrote into.
	deps map[string][]string
	temp []string
}

// NewRunner prepares to run cases against the configuration in `flags`.
func NewRunner(flags *core.CLIFlags) *Runner {
	fsys := system.Default
	if flags != nil && flags.FS != nil {
		fsys = flags.FS
	}
	return &Runner{flags: flags, fs: fsys, deps: map[string][]string{}}
}

// Close removes the packages installed for the run.
func (r *Runner) Close() {
	for _, dir := range r.temp {
		_ = os.RemoveAll(dir)
	}
	r.temp = nil
}

// Run lints one case and compares what came back.
func (r *Runner) Run(c Case) Result {
	if c.Lang != "" && c.Rule == "" {
		return Result{Case: c, Err: fmt.Errorf("%s: %q: `lang` needs `rule`; a project case takes its language from the project's sections",
			filepath.Base(c.Path), c.Name)}
	}

	got, sequence, err := r.lint(c)
	if err != nil {
		return Result{Case: c, Err: err}
	}

	result := Result{Case: c, Got: got, Reason: compare(c, got)}
	if result.Reason != "" && sequence {
		result.Tags = tagged(c)
	}
	return result
}

// lint converts a case's input and returns its alerts, one per line, and
// whether a `sequence` rule was among those run.
func (r *Runner) lint(c Case) (string, bool, error) {
	linter, err := r.linterFor(c)
	if err != nil {
		return "", false, err
	}

	linter.Manager.Config.Flags.InExt = c.Ext()

	linted, err := linter.LintString(c.Input)
	if err != nil {
		return "", false, err
	}

	var out strings.Builder
	for _, f := range linted {
		for _, a := range f.SortedAlerts() {
			fmt.Fprintf(&out, "%d:%d:%s:%s\n", a.Line, a.Span[0], a.Check, a.Message)
		}
	}

	sequence := false
	for _, rule := range linter.Manager.Rules() {
		if rule.Fields().Extends == "sequence" {
			sequence = true
			break
		}
	}

	return out.String(), sequence, nil
}

// tagged is a case's input as the tagger reads it, `word/TAG` per token,
// in the case's language. Markup is tagged as written: a sequence case is
// almost always plain sentences, and the point is to see the tags the rule
// saw.
func tagged(c Case) string {
	tokens, err := nlp.TextToTokens(strings.TrimSpace(c.Input), &nlp.Info{Lang: c.Lang})
	if err != nil {
		return ""
	}
	parts := make([]string, 0, len(tokens))
	for _, t := range tokens {
		parts = append(parts, t.Text+"/"+t.Tag)
	}
	return strings.Join(parts, " ")
}

// linterFor returns the linter a case runs under.
func (r *Runner) linterFor(c Case) (*lint.Linter, error) {
	if c.Rule != "" {
		search, err := r.isolatedSearch(c)
		if err != nil {
			return nil, err
		}
		return isolate(c, search, r.fs)
	}

	if r.project == nil {
		cfg, err := r.config()
		if err != nil {
			return nil, err
		}

		r.project, err = lint.NewLinter(cfg)
		if err != nil {
			return nil, err
		}
	}

	return r.project, nil
}

// searchPaths reads the configuration's StylesPath list, once, and swallows
// the error: a directory of cases with no .vale.ini anywhere is a supported
// way to run, and naming falls back to the rule's parent directory there.
func (r *Runner) searchPaths() []string {
	if !r.pathsSet {
		r.pathsSet = true
		if cfg, err := r.config(); err == nil {
			r.paths = cfg.SearchPaths()
		}
	}
	return r.paths
}

// config is the project's configuration: the one handed in, or the one
// `flags` finds on disk.
func (r *Runner) config() (*core.Config, error) {
	if r.cfg != nil {
		return r.cfg, nil
	}
	return core.ReadPipeline(r.flags, false)
}

// isolatedSearch orders the search paths for an isolated case: installed
// dependencies first, then the project's paths, and the rule's own package
// last, since the last path is the StylesPath its dictionaries are read from.
func (r *Runner) isolatedSearch(c Case) ([]string, error) {
	pkg, err := r.packagePaths(c)
	if err != nil || len(pkg) == 0 {
		return r.searchPaths(), err
	}

	own := pkg[0]
	search := append([]string{}, pkg[1:]...)
	for _, sp := range r.searchPaths() {
		if abs, aErr := filepath.Abs(sp); aErr != nil || abs != own {
			search = append(search, sp)
		}
	}
	return append(search, own), nil
}

// packagePaths returns the search paths a rule's package brings: its own
// `styles` directory first, then a StylesPath holding the packages its
// `.vale.ini` declares that the search paths don't already have. A package
// root is the nearest directory above the rule with both a `.vale.ini` and a
// `styles` directory, the layout `vale sync` installs.
func (r *Runner) packagePaths(c Case) ([]string, error) {
	rule := c.Rule
	if !filepath.IsAbs(rule) {
		rule = filepath.Join(filepath.Dir(c.Path), rule)
	}
	rule, err := filepath.Abs(rule)
	if err != nil {
		return nil, err
	}

	root := packageRoot(filepath.Dir(rule))
	if root == "" {
		return nil, nil
	} else if paths, ok := r.deps[root]; ok {
		return paths, nil
	}

	styles := filepath.Join(root, "styles")
	paths := []string{styles}

	pkgs, err := core.GetPackages(filepath.Join(root, ".vale.ini"))
	if err != nil {
		return nil, err
	}

	var missing []string
	for _, pkg := range pkgs {
		if !onPath(system.FileNameWithoutExt(pkg), append([]string{styles}, r.searchPaths()...)) {
			missing = append(missing, pkg)
		}
	}

	if len(missing) > 0 && r.Install != nil {
		dir, tErr := os.MkdirTemp("", "vale-test")
		if tErr != nil {
			return nil, tErr
		}
		r.temp = append(r.temp, dir)
		if err = r.Install(missing, dir); err != nil {
			return nil, err
		}
		paths = append(paths, dir)
	}

	r.deps[root] = paths
	return paths, nil
}

// packageRoot walks up from dir to the nearest package root, or "".
func packageRoot(dir string) string {
	for {
		if system.FileExists(filepath.Join(dir, ".vale.ini")) &&
			system.IsDir(filepath.Join(dir, "styles")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// onPath reports whether a style named name is installed on a search path.
func onPath(name string, search []string) bool {
	for _, sp := range search {
		if system.IsDir(filepath.Join(sp, name)) {
			return true
		}
	}
	return false
}

// isolate builds a linter holding one rule and nothing else.
//
// The rule keeps the name a project run would give it -- found under a search
// path, that is its full path under the style root (`Std.dates.TimeFormat`);
// otherwise the parent directory is the style, as before. Either way a case
// can be moved between the two modes without its `want` changing.
func isolate(c Case, search []string, fsys system.FS) (*lint.Linter, error) {
	path := c.Rule
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(c.Path), path)
	}

	name := isolatedName(path, search)
	style := core.StyleName(name)

	cfg, err := core.NewConfig(&core.CLIFlags{FS: fsys})
	if err != nil {
		return nil, err
	}

	// Every severity, since an isolated rule is being asked what it matches
	// and not whether a run would show it.
	cfg.MinAlertLevel = 0
	cfg.GBaseStyles = []string{style}
	if c.Lang != "" {
		cfg.FormatToLang["*"] = c.Lang
	}

	// A rule that extends another rule resolves its parent against the search
	// paths; isolation changes what runs, not what a reference means.
	for _, sp := range search {
		cfg.AddStylesPath(sp)
	}

	// A View is attached to every file of the case's format, which is what
	// a section would do; it has to be there before the rule compiles, since
	// a rule's `in` is checked against the Views that exist.
	if c.View != "" {
		viewPath := core.FindConfigAsset(cfg, c.View+".yml", core.ViewDir)
		if viewPath == "" {
			return nil, fmt.Errorf("%s: %q: view '%s' not found under config/views on the search path",
				filepath.Base(c.Path), c.Name, c.View)
		}
		view, vErr := core.NewView(viewPath, cfg.FS)
		if vErr != nil {
			return nil, vErr
		}
		cfg.Views["*"+c.Ext()] = view
	}

	linter, err := lint.NewLinter(cfg)
	if err != nil {
		return nil, err
	}

	if err = linter.Manager.AddRuleFromFile(name, path); err != nil {
		return nil, err
	}

	return linter, nil
}

// isolatedName names the rule at path the way a project run would.
func isolatedName(path string, search []string) string {
	if abs, err := filepath.Abs(path); err == nil {
		for _, sp := range search {
			spAbs, aErr := filepath.Abs(sp)
			if aErr != nil {
				continue
			}

			rel, rErr := filepath.Rel(spAbs, abs)
			if rErr != nil || strings.HasPrefix(rel, "..") || !strings.ContainsRune(rel, filepath.Separator) {
				// Not under this search path, or sitting directly in it with
				// no style directory to take a name from.
				continue
			}

			root := filepath.Join(spAbs, strings.Split(filepath.ToSlash(rel), "/")[0])
			if name, cErr := core.CheckName(root, abs); cErr == nil {
				return name
			}
		}
	}

	style := filepath.Base(filepath.Dir(path))
	base := strings.Split(filepath.Base(path), ".")[0]
	return style + "." + base
}

// compare returns why a case failed, or "" if it passed.
func compare(c Case, got string) string {
	if c.Want != nil {
		if want := strings.TrimSpace(*c.Want); strings.TrimSpace(got) != want {
			return "output does not match"
		}
	}

	for _, want := range c.Contains {
		if !strings.Contains(got, want) {
			return fmt.Sprintf("output does not contain %q", want)
		}
	}

	for _, absent := range c.Absent {
		if strings.Contains(got, absent) {
			return fmt.Sprintf("output contains %q", absent)
		}
	}

	return ""
}
