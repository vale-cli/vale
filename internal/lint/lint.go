package lint

import (
	"errors"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/remeh/sizedwaitgroup"

	"github.com/errata-ai/vale/v3/internal/check"
	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/glob"
	"github.com/errata-ai/vale/v3/internal/nlp"
	"github.com/errata-ai/vale/v3/internal/system"
)

// A Linter lints a File.
type Linter struct {
	Manager   *check.Manager
	glob      *glob.Glob
	client    *http.Client
	HasDir    bool
	nonGlobal bool
	metaScope string
}

type lintResult struct {
	file *core.File
	err  error
}

// NewLinter initializes a Linter.
func NewLinter(cfg *core.Config) (*Linter, error) {
	mgr, err := check.NewManager(cfg)

	globalStyles := len(cfg.GBaseStyles)
	globalChecks := len(cfg.GChecks)

	return &Linter{
		Manager: mgr,

		client:    http.DefaultClient,
		nonGlobal: globalStyles+globalChecks == 0}, err
}

// Transform applies the configured transformations to text and returns the
// result.
//
// This is used by the `vale` command to apply transformations to text before
// linting it.
//
// Transformations include block and token ignores, as well as some built-in
// replacements.
func (l *Linter) Transform(f *core.File) (string, error) {
	exts := extensionConfig{
		Normed: f.NormedExt,
		Real:   f.RealExt,
	}

	return applyPatterns(l.Manager.Config, exts, f.Content)
}

// LintString src according to its format.
func (l *Linter) LintString(src string) ([]*core.File, error) {
	linted := l.lintFile(src)
	return []*core.File{linted.file}, linted.err
}

// SetMetaScope sets an optional meta scope.
//
// A meta scope is a string that is appended to the end of each check's scope
// providing extra context for the check.
func (l *Linter) SetMetaScope(scope string) {
	if scope != "" {
		l.metaScope = "." + scope
	} else {
		l.metaScope = ""
	}
}

// Lint src according to its format.
func (l *Linter) Lint(input []string, pat string) ([]*core.File, error) {
	var linted []*core.File

	done := make(chan core.File)
	defer close(done)

	gp, err := glob.NewGlob(pat)
	if err != nil {
		return linted, err
	}

	l.glob = &gp
	for _, src := range input {
		filesChan, errChan := l.lintFiles(done, src)

		for result := range filesChan {
			if result.err != nil {
				return linted, result.err
			} else if l.Manager.Config.Flags.Normalize {
				result.file.Path = filepath.ToSlash(result.file.Path)
			}
			linted = append(linted, result.file)
		}

		if err = <-errChan; err != nil {
			return linted, err
		}
	}

	return linted, nil
}

// lintFiles walks the `root` directory, creating a new goroutine to lint any
// file that matches the given glob pattern.
func (l *Linter) lintFiles(done <-chan core.File, root string) (<-chan lintResult, <-chan error) {
	filesChan := make(chan lintResult)
	errChan := make(chan error, 1)

	go func() {
		wg := sizedwaitgroup.New(5)

		err := system.Walk(root, func(fp string, info fs.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if info.IsDir() && core.ShouldIgnoreDirectory(fp) {
				return filepath.SkipDir
			} else if info.IsDir() || l.skip(fp) {
				return nil
			}

			wg.Add()
			go func(fp string) {
				select {
				case filesChan <- l.lintFile(fp):
				case <-done:
				}
				wg.Done()
			}(fp)

			// Abort the walk if done is closed.
			select {
			case <-done:
				return errors.New("walk canceled")
			default:
				return nil
			}
		})

		// Walk has returned, so all calls to wg.Add are done.  Start a
		// goroutine to close c once all the sends are done.
		go func() {
			wg.Wait()
			close(filesChan)
		}()
		errChan <- err
	}()

	return filesChan, errChan
}

// lintFile creates a new `File` from the path `src` and selects a linter based
// on its format.
func (l *Linter) lintFile(src string) lintResult {
	var err error

	file, err := core.NewFile(src, l.Manager.Config)
	if err != nil {
		return lintResult{err: err}
	} else if len(file.Checks) == 0 && len(file.BaseStyles) == 0 {
		if len(l.Manager.Config.GBaseStyles) == 0 && len(l.Manager.Config.GChecks) == 0 {
			// There's nothing to do; bail early.
			return lintResult{file: file}
		}
	}

	// Determine what NLP tasks this particular file needs; the goal is to do
	// the least amount of work possible.
	file.NLP = l.Manager.AssignNLP(file)
	simple := l.Manager.Config.Flags.Simple

	// NOTE: This is a sanity check to ensure that we don't run any checks that
	// we actually have a View to apply.
	hasViews := len(l.Manager.Config.Views) > 0

	if file.Format == "markup" && !simple { //nolint:gocritic
		switch file.NormedExt {
		case ".adoc":
			err = l.lintADoc(file)
		case ".md":
			err = l.lintMarkdown(file)
		case ".mdx":
			err = l.lintMDX(file)
		case ".rst":
			err = l.lintRST(file)
		case ".xml", ".xsd":
			err = l.lintXML(file)
		case ".dita":
			err = l.lintDITA(file)
		case ".html":
			err = l.lintHTML(file)
		case ".org":
			err = l.lintOrg(file)
		}
	} else if file.Format == "data" && !simple && hasViews {
		err = l.lintData(file)
	} else if file.Format == "code" && !simple {
		err = l.lintCode(file)
	} else if file.Format == "fragment" && !simple {
		err = l.lintFragments(file)
	} else if file.NormedExt == ".txt" && !simple {
		err = l.lintTxt(file)
	} else {
		err = l.lintLines(file)
	}

	if err == nil {
		// Run all rules with `scope: raw`
		//
		// NOTE: We need to use `f.Lines` (instead of `f.Content`) to ensure
		// that we don't include any markup preprocessing.
		//
		// See #248, #306.
		raw := nlp.NewBlock("", strings.Join(file.Lines, ""), "raw"+file.RealExt)
		err = l.lintBlock(file, raw, len(file.Lines), 0, true)
	}

	return lintResult{file, err}
}

func (l *Linter) lintProse(f *core.File, blk nlp.Block, lines int) error {
	blks, err := f.NLP.Compute(&blk)
	if err != nil {
		return core.NewE100("NLP.Compute", err)
	}

	// FIXME: This is required for paragraphs that lack a newline delimiter:
	//
	// p1
	// p2
	//
	// See fixtures/i18n for an example.
	needsLookup := strings.Count(blk.Text, "\n") > 0 || f.Lookup
	for _, b := range blks {
		err = l.lintBlock(f, b, lines, 0, needsLookup)
		if err != nil {
			return err
		}
	}

	return nil
}

func (l *Linter) lintTxt(f *core.File) error {
	block := nlp.NewBlock("", f.Content, "text"+l.metaScope+f.RealExt)
	return l.lintProse(f, block, len(f.Lines))
}

func (l *Linter) lintLines(f *core.File) error {
	block := nlp.NewBlock("", f.Content, "text"+l.metaScope+f.RealExt)
	return l.lintBlock(f, block, len(f.Lines), 0, true)
}

func (l *Linter) lintBlock(f *core.File, blk nlp.Block, lines, pad int, lookup bool) error {
	f.ChkToCtx = make(map[string]string)
	for name, chk := range l.Manager.Rules() {
		if !l.shouldRun(name, f, chk, blk) {
			continue
		}

		info := chk.Fields()

		alerts, err := chk.Run(blk, f, l.Manager.Config)
		if err != nil {
			return err
		}
		for i := range alerts {
			if f.QueryComments(name + "[" + alerts[i].Match + "]") {
				continue
			}
			core.FormatAlert(&alerts[i], info.Limit, info.Level, name)
			f.AddAlert(alerts[i], blk, lines, pad, lookup)
		}
	}

	return nil
}

func (l *Linter) shouldRun(name string, f *core.File, chk check.Rule, blk nlp.Block) bool {
	minLevel := l.Manager.Config.MinAlertLevel
	run := false

	details := chk.Fields()
	if strings.Count(name, ".") > 1 {
		// NOTE: This fixes the loading issue with consistency checks.
		//
		// See #129.
		list := strings.Split(name, ".")
		name = strings.Join([]string{list[0], list[1]}, ".")
	}

	chkScope := check.NewScope(details.Scope)
	if f.QueryComments(name) { //nolint:gocritic
		// It has been disabled via an in-text comment.
		return false
	} else if core.LevelToInt[details.Level] < minLevel {
		return false
	} else if !chkScope.Matches(blk) {
		return false
	}

	// Has the check been disabled for this extension?
	if val, ok := f.Checks[name]; ok && !run {
		if !val {
			return false
		}
		run = true
	}

	// Has the check been disabled for all extensions?
	if val, ok := l.Manager.Config.GChecks[name]; ok && !run {
		if !val {
			return false
		}
		run = true
	}

	style := strings.Split(name, ".")[0]
	if !run && !core.StringInSlice(style, f.BaseStyles) {
		return false
	}

	return true
}

func (l *Linter) match(s string) bool {
	if l.glob == nil {
		return true
	}
	return l.glob.Match(s)
}

func (l *Linter) skip(old string) bool {
	ref := filepath.ToSlash(system.ReplaceFileExt(old, l.Manager.Config.Formats))

	if !l.match(old) && !l.match(ref) {
		return true
	} else if l.nonGlobal {
		for _, pat := range l.Manager.Config.SecToPat {
			if pat.Match(old) || pat.Match(ref) {
				return false
			}
		}
		return true
	}

	return false
}
