package testsuite

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/lint"
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
}

// NewRunner prepares to run cases against the configuration in `flags`.
func NewRunner(flags *core.CLIFlags) *Runner {
	return &Runner{flags: flags}
}

// Run lints one case and compares what came back.
func (r *Runner) Run(c Case) Result {
	got, err := r.lint(c)
	if err != nil {
		return Result{Case: c, Err: err}
	}

	return Result{Case: c, Got: got, Reason: compare(c, got)}
}

// lint converts a case's input and returns its alerts, one per line.
func (r *Runner) lint(c Case) (string, error) {
	linter, err := r.linterFor(c)
	if err != nil {
		return "", err
	}

	linter.Manager.Config.Flags.InExt = c.Ext()

	linted, err := linter.LintString(c.Input)
	if err != nil {
		return "", err
	}

	var out strings.Builder
	for _, f := range linted {
		for _, a := range f.SortedAlerts() {
			fmt.Fprintf(&out, "%d:%d:%s:%s\n", a.Line, a.Span[0], a.Check, a.Message)
		}
	}

	return out.String(), nil
}

// linterFor returns the linter a case runs under.
func (r *Runner) linterFor(c Case) (*lint.Linter, error) {
	if c.Rule != "" {
		return isolate(c, r.searchPaths())
	}

	if r.project == nil {
		cfg, err := core.ReadPipeline(r.flags, false)
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
		if cfg, err := core.ReadPipeline(r.flags, false); err == nil {
			r.paths = cfg.SearchPaths()
		}
	}
	return r.paths
}

// isolate builds a linter holding one rule and nothing else.
//
// The rule keeps the name a project run would give it -- found under a search
// path, that is its full path under the style root (`Std.dates.TimeFormat`);
// otherwise the parent directory is the style, as before. Either way a case
// can be moved between the two modes without its `want` changing.
func isolate(c Case, search []string) (*lint.Linter, error) {
	path := c.Rule
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(c.Path), path)
	}

	name := isolatedName(path, search)
	style := core.StyleName(name)

	cfg, err := core.NewConfig(&core.CLIFlags{})
	if err != nil {
		return nil, err
	}

	// Every severity, since an isolated rule is being asked what it matches
	// and not whether a run would show it.
	cfg.MinAlertLevel = 0
	cfg.GBaseStyles = []string{style}

	// A rule that extends another rule resolves its parent against the search
	// paths; isolation changes what runs, not what a reference means.
	for _, sp := range search {
		cfg.AddStylesPath(sp)
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

	if c.Contains != "" && !strings.Contains(got, c.Contains) {
		return fmt.Sprintf("output does not contain %q", c.Contains)
	}

	for _, absent := range c.Absent {
		if strings.Contains(got, absent) {
			return fmt.Sprintf("output contains %q", absent)
		}
	}

	return ""
}
