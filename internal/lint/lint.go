package lint

import (
	"errors"
	"io/fs"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/remeh/sizedwaitgroup"

	"github.com/vale-cli/vale/v3/internal/check"
	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/glob"
	"github.com/vale-cli/vale/v3/internal/nlp"
	"github.com/vale-cli/vale/v3/internal/system"
)

// A Linter lints a File.
type Linter struct {
	Manager   *check.Manager
	glob      *glob.Glob
	nonGlobal bool

	// BlockHook, when set, receives every block before its rules run.
	BlockHook func(nlp.Block)

	// RuleHook, when set, receives each rule's name and what its Run cost.
	// A hooked run takes the serial path, which is the one that can time a rule.
	RuleHook func(name string, took time.Duration)

	// ditaHTML holds what the DITA toolkit made of each DITA file in the
	// run, by absolute path, from one conversion of them all. See
	// prepareDITA.
	ditaHTML map[string][]byte

	// adoc holds the Asciidoctor processes this run is using, and adocOnce
	// starts them the first time an AsciiDoc file is seen.
	//
	// These belong to the Linter rather than to the package: a long-lived
	// caller -- the language server, or anything embedding Vale -- lints many
	// times in one process, and a pool with no owner is a pool nothing ever
	// stops.
	adoc     *procPool
	adocOnce sync.Once

	// typst is the same arrangement for typst2vast.
	typst         *procPool
	typstPoolOnce sync.Once

	// rst is the same arrangement for Docutils.
	rst     *procPool
	rstOnce sync.Once

	// singleDoc marks a run that lints exactly one document.
	//
	// The pools above are sized for a walk over many files: one interpreter
	// per file in flight. A run holding a single document would start that
	// many, convert one document, and stop them again -- strictly more work
	// than the one process the unpooled path used. Such a run keeps one.
	singleDoc bool

	// inScope lists the rules whose scope matches a given block scope, keyed by
	// the block's scope and parent.
	//
	// Whether a rule's scope matches depends on nothing else, and a document
	// has a handful of distinct block scopes against several hundred rules --
	// so the answer was being recomputed for every rule on every block, which
	// was the largest part of deciding what to run.
	inScope *sync.Map
}

// scopedRule is a rule together with the name it was registered under.
type scopedRule struct {
	name string
	rule check.Rule
}

type lintResult struct {
	file *core.File
	err  error
	path string // the document linted, for an error that is about it
}

// DocumentErrors are the documents a run couldn't lint, each with its own
// error. Every other document was linted.
type DocumentErrors []error

func (e DocumentErrors) Error() string {
	msgs := make([]string, len(e))
	for i, err := range e {
		msgs[i] = err.Error()
	}
	return strings.Join(msgs, "\n")
}

// documentError reports whether err is about the document at path, one it
// couldn't read or parse, rather than about the configuration or a rule.
func documentError(err error, path string) bool {
	var ve *core.Error
	if path == "" || !errors.As(err, &ve) {
		return false
	}
	for _, p := range []string{ve.Path, ve.Context} {
		if p != "" && filepath.Clean(p) == filepath.Clean(path) {
			return true
		}
	}
	return false
}

// NewLinter initializes a Linter.
func NewLinter(cfg *core.Config) (*Linter, error) {
	if err := core.LoadLanguageModels(cfg); err != nil {
		return nil, err
	}
	mgr, err := check.NewManager(cfg)

	globalStyles := len(cfg.GBaseStyles)
	globalChecks := len(cfg.GChecks)

	return &Linter{
		Manager: mgr,
		inScope: &sync.Map{},

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
		Normed:   f.NormedExt,
		Real:     f.RealExt,
		RealPath: f.Path,
		Held:     f.Held,
	}

	return applyPatterns(l.Manager.Config, exts, f.Content)
}

// poolSize is how many helper processes to keep warm for this run.
func (l *Linter) poolSize() int {
	if l.singleDoc {
		return 1
	}
	return adocConcurrency
}

// LintString src according to its format.
func (l *Linter) LintString(src string) ([]*core.File, error) {
	l.singleDoc = true

	// A string is linted by the same helpers a file is -- `cat page.rst |
	// vale` reaches Docutils exactly as `vale page.rst` does -- so it owes
	// them the same shutdown. Without this the run left its processes to be
	// noticed by the closing of a pipe rather than being waited for, which is
	// the one way this path differed from Lint.
	defer l.stopExternal()

	linted := l.lintFile(src)
	return []*core.File{linted.file}, linted.err
}

// LintStringAs lints content as the document named virtualName, whose
// extension says how it is read. It is how a host with no filesystem, such
// as a browser, lints.
func (l *Linter) LintStringAs(content, virtualName string) ([]*core.File, error) {
	l.singleDoc = true
	defer l.stopExternal()

	file, err := core.NewFileFromContent(content, virtualName, l.Manager.Config)
	if err != nil {
		return nil, err
	}

	linted := l.lintPreparedFile(file)
	return []*core.File{linted.file}, linted.err
}

// LintBuilt lints content as the document named virtualName, from the HTML
// that a converter the host ran produced for it. It is how a host without
// the converter Vale would run, such as a browser with Asciidoctor.js, lints
// a format Vale reads through one.
func (l *Linter) LintBuilt(content, virtualName, html string) ([]*core.File, error) {
	l.singleDoc = true
	defer l.stopExternal()

	file, err := core.NewFileFromContent(content, virtualName, l.Manager.Config)
	if err != nil {
		return nil, err
	}

	linted := l.lintPrepared(file, []byte(html))
	return []*core.File{linted.file}, linted.err
}

// Lint src according to its format.
func (l *Linter) Lint(input []string, pat string) ([]*core.File, error) {
	var linted []*core.File

	done := make(chan core.File)
	defer close(done)

	gp, err := glob.NewGlob(pat)
	if err != nil {
		return linted, core.NewE100("--glob", err)
	}

	l.glob = &gp

	// `vale README.adoc` is as much a single-document run as linting a string
	// is; only a directory can turn into the concurrent walk the pools are
	// sized for.
	l.singleDoc = len(input) == 1 && !system.IsDir(input[0])

	// Whatever external processes this run starts, it also stops. Lint may be
	// called again on the same Linter, so the next run starts its own.
	defer l.stopExternal()

	l.prepareDITA(input)
	defer func() { l.ditaHTML = nil }()

	// A document that can't be read or parsed is reported, and the rest are
	// still linted; anything else stops the run.
	var docErrs DocumentErrors
	for _, src := range input {
		filesChan, errChan := l.lintFiles(done, src)

		for result := range filesChan {
			switch {
			case result.err != nil && documentError(result.err, result.path):
				docErrs = append(docErrs, result.err)
				continue
			case result.err != nil:
				return linted, result.err
			case l.Manager.Config.Flags.Normalize:
				result.file.Path = filepath.ToSlash(result.file.Path)
			}
			linted = append(linted, result.file)
		}

		if err = <-errChan; err != nil {
			return linted, err
		}
	}

	if len(docErrs) > 0 {
		// Files are linted concurrently, so their errors arrive in any order.
		sort.SliceStable(docErrs, func(i, j int) bool {
			return errorPath(docErrs[i]) < errorPath(docErrs[j])
		})
		return linted, docErrs
	}
	return linted, nil
}

// errorPath is the document an error from documentError is about.
func errorPath(err error) string {
	var ve *core.Error
	if errors.As(err, &ve) {
		if ve.Path != "" {
			return ve.Path
		}
		return ve.Context
	}
	return ""
}

// lintFiles walks the `root` directory, creating a new goroutine to lint any
// file that matches the given glob pattern.
func (l *Linter) lintFiles(done <-chan core.File, root string) (<-chan lintResult, <-chan error) {
	filesChan := make(chan lintResult)
	errChan := make(chan error, 1)

	go func() {
		wg := sizedwaitgroup.New(5)

		err := l.Manager.Config.FS.Walk(root, func(fp string, info fs.FileInfo, err error) error {
			if err != nil {
				return err
			}

			if info.IsDir() && (core.ShouldIgnoreDirectory(fp) || (fp != root && l.isStylesPath(fp))) {
				// A StylesPath holds rules, vocabularies, and synced
				// packages, not prose; it is linted only when named.
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
	file, err := core.NewFile(src, l.Manager.Config)
	if err != nil {
		return lintResult{err: err, path: src}
	}
	return l.lintPreparedFile(file)
}

// lintPreparedFile lints a File already built, from disk or from memory.
func (l *Linter) lintPreparedFile(file *core.File) lintResult {
	return l.lintPrepared(file, nil)
}

// lintPrepared lints file; built is the HTML a converter the host ran
// produced for it, or nil when Vale runs the converter itself.
func (l *Linter) lintPrepared(file *core.File, built []byte) lintResult {
	var err error

	if l.BlockHook == nil && l.runsNothing(file) {
		// No rule can run here -- `BasedOnStyles =` in a section that
		// matched, say -- so the file isn't parsed at all. A BlockHook
		// still wants the block stream, so it always parses.
		return lintResult{file: file}
	}

	// Determine what NLP tasks this particular file needs; the goal is to do
	// the least amount of work possible. The manager answers for the whole
	// run, so a file is only segmented when a rule asking for it runs there.
	file.NLP = l.Manager.AssignNLP(file)
	file.NLP.Segmentation = file.NLP.Segmentation && l.runsScoped(file, "sentence")
	file.NLP.Splitting = file.NLP.Splitting && l.runsScoped(file, "paragraph")
	simple := l.Manager.Config.Flags.Simple

	// NOTE: This is a sanity check to ensure that we don't run any checks that
	// we actually have a View to apply.
	hasViews := len(l.Manager.Config.Views) > 0

	if built != nil { //nolint:gocritic
		err = l.lintBuilt(file, built)
	} else if !simple && l.hasView(file) {
		// A file a view reads: the view says what its prose is, whatever the
		// file is called. Without this a `.jsonl` or `.log` the section
		// matched was linted whole, and the view never ran.
		err = l.lintData(file)
	} else if file.Format == "markup" && !simple {
		switch file.NormedExt {
		case ".adoc":
			err = l.lintADoc(file)
		case ".md":
			err = l.lintMarkdown(file)
		case ".mdx":
			err = l.lintMDX(file)
		case ".myst":
			err = l.lintMyST(file)
		case ".qdoc":
			err = l.lintQDoc(file)
		case ".qmd":
			err = l.lintQuarto(file)
		case ".rst":
			err = l.lintRST(file)
		case ".typ":
			err = l.lintTypst(file)
		case ".xml", ".xsd":
			err = l.lintXML(file)
		case ".dita":
			err = l.lintDITA(file)
		case ".html":
			err = l.lintHTML(file)
		case ".ipynb":
			err = l.lintNotebook(file)
		case ".org":
			err = l.lintOrg(file)
		}
	} else if !simple && isRuleFile(file) {
		// A Vale rule: its message and description are prose, and its
		// tokens and swaps are not.
		err = l.lintRule(file)
	} else if file.Format == "data" && !simple && l.hasCodeView(file) {
		// A data file a tree-sitter View names is read as code, so the
		// View's queries reach its comments. See #1188.
		err = l.lintCode(file)
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

	// A comment read after a block was linted still covers that block: a
	// converter may emit a promoted title ahead of the comment above it.
	file.DropDisabled()

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

	// A transformed file's alerts live in the stylesheet's output, which the
	// sanitizer's shifts say nothing about.
	if err == nil && file.Transform == "" {
		file.MapAlertsToSource()
	}

	return lintResult{file: file, err: err, path: file.Path}
}

// runsNothing reports whether no rule can run for f: it bases on no style,
// and no rule is switched on by name, in a section or globally. It errs
// toward false; shouldRun has the final say on each rule.
func (l *Linter) runsNothing(f *core.File) bool {
	if len(f.BaseStyles) > 0 {
		return false
	}
	for _, on := range f.Checks {
		if on {
			return false
		}
	}
	for k, on := range l.Manager.Config.GChecks {
		if v, set := f.Checks[k]; on && !f.Unset[k] && (!set || v) {
			return false
		}
	}
	return true
}

// lintProse segments blk and runs every applicable rule over the results.
//
// split says whether blk holds paragraphs; a heading or a list item is prose,
// but not a paragraph. See nlp.Info.Compute.
func (l *Linter) lintProse(f *core.File, blk nlp.Block, lines int, split bool) error {
	blks, err := f.NLP.Compute(&blk, split)
	if err != nil {
		return core.NewE100(f.Path, err)
	}

	// FIXME: This is required for paragraphs that lack a newline delimiter:
	//
	// p1
	// p2
	//
	// See fixtures/i18n for an example.
	needsLookup := strings.Count(blk.Text, "\n") > 0 || f.Lookup

	// Segmenting hands back blocks that differ in scope but not always in
	// text, so a rule matching both runs twice over the same string. The second
	// run reports nothing the first did not, and it was once worth tracking
	// which rules had already seen a text to skip it.
	//
	// It no longer is: the prefilter now turns a repeat away cheaply, leaving
	// the bookkeeping to cost more than the work it saved -- 38% of the peak
	// memory on a plain-text run, for no time back.
	for _, b := range blks {
		err = l.lintBlock(f, b, lines, 0, needsLookup)
		if err != nil {
			return err
		}
	}

	return nil
}

func (l *Linter) lintTxt(f *core.File) error {
	// A manuscript with chapters is a document: its chapter lines are
	// headings, and each one opens a section. See txtdoc.go.
	if doc, ok := textToHTML(f.Content); ok {
		return l.lintHTMLTokens(f, []byte(doc), 0)
	}

	block := nlp.NewBlock("", f.Content, "text"+f.MetaScope+f.RealExt)

	// Plain text never becomes HTML, so its quotations are paired here and
	// carried as inline runs, the way the walker carries a `q`.
	if l.Manager.HasScope("quote") {
		for _, span := range nlp.QuoteSpans(f.Content) {
			block.Inline = append(block.Inline, nlp.Inline{Scope: "quote", Begin: span[0], End: span[1]})
		}
	}

	if err := l.lintProse(f, block, len(f.Lines), true); err != nil {
		return err
	}

	for _, in := range block.Inline {
		text := f.Content[in.Begin:in.End]
		line := 1 + strings.Count(f.Content[:in.Begin], "\n")
		b := nlp.NewLinedBlock(f.Content, text, "quote"+f.MetaScope+f.RealExt, line)
		b.Offset = in.Begin
		if err := l.lintBlock(f, b, len(f.Lines), 0, false); err != nil {
			return err
		}
	}

	// No walker writes a plain-text file's summary, so the file is its own:
	// one run covering it, and a paragraph per run of lines between blank
	// lines. Without this a `metric` rule never fired on `.txt`, and
	// `ls-metrics` printed nothing for it.
	f.Summary.WriteString(f.Content)
	f.SummaryRuns = []nlp.Run{{At: 0, Src: 0, N: len(f.Content)}}
	f.Metrics["paragraphs"] = countParagraphs(f.Content)
	return l.lintSizedScopes(f)
}

// countParagraphs counts the runs of non-blank lines in plain text.
func countParagraphs(text string) int {
	n := 0
	for _, p := range strings.Split(text, "\n\n") {
		if strings.TrimSpace(p) != "" {
			n++
		}
	}
	return n
}

func (l *Linter) lintLines(f *core.File) error {
	block := nlp.NewBlock("", f.Content, "text"+f.MetaScope+f.RealExt)
	return l.lintBlock(f, block, len(f.Lines), 0, true)
}

// concurrentKinds names the extension points whose Run reads nothing but the
// block it is given.
//
// The rest reach into the file: `consistency` and `conditional` accumulate
// matches on it, `sequence` tags through a cache it owns, `metric` reads its
// counts, and `spelling` holds a dictionary of its own. Those keep the serial
// pass.
var concurrentKinds = map[string]bool{
	"capitalization": true,
	"existence":      true,
	"occurrence":     true,
	"readability":    true,
	"repetition":     true,
	"script":         true,
	"substitution":   true,
}

// blockWorkers bounds rule concurrency across the whole run, not per block:
// files are already linted in parallel, and a pool per block would multiply by
// however many are in flight.
var blockWorkers = make(chan struct{}, runtime.GOMAXPROCS(0))

// parallelFloor is the block size below which running rules concurrently costs
// more than it saves. A variable so a test can force either path over the same
// input.
var parallelFloor = 4096

// lintBlock runs every applicable rule over blk.
//
// Rules that only read the block run concurrently; the rest run in order
// afterwards. Alerts are added in rule order either way, so which rule wins a
// span, and what `ChkToCtx` holds when a later one is formatted, do not depend
// on the scheduler.
func (l *Linter) lintBlock(f *core.File, blk nlp.Block, lines, pad int, lookup bool) error {
	f.StartBlock()

	if l.BlockHook != nil {
		l.BlockHook(blk)
	}

	rules := l.inScopeFor(blk)

	// Below the floor the bookkeeping concurrency needs -- two slices the
	// length of the rule set, per block -- costs more than the rules do. Most
	// blocks are a paragraph.
	if len(blk.Text) < parallelFloor || l.RuleHook != nil {
		return l.lintBlockSerial(f, blk, rules, lines, pad, lookup)
	}

	found := make([][]core.Alert, len(rules))
	wanted := make([]bool, len(rules))

	var todo []int
	for i, r := range rules {
		if !l.shouldRun(r.name, f, r.rule) {
			continue
		}
		wanted[i] = true
		if concurrentKinds[r.rule.Fields().Extends] {
			todo = append(todo, i)
		}
	}

	if err := l.runConcurrently(f, blk, rules, todo, found); err != nil {
		return err
	}

	for i, r := range rules {
		if !wanted[i] || found[i] != nil {
			continue
		}
		alerts, err := r.rule.Run(blockFor(blk, r.rule), f, l.Manager.Config)
		if err != nil {
			return err
		}
		found[i] = alerts
	}

	for i, r := range rules {
		if !wanted[i] {
			continue
		}
		info := r.rule.Fields()
		for j := range found[i] {
			if f.QueryComments(r.name + "[" + found[i][j].Match + "]") {
				continue
			}
			setLevel(&found[i][j], f, info, r.name)
			f.AddAlert(found[i][j], blk, lines, pad, lookup)
		}
	}

	return nil
}

// setLevel finishes an alert, reporting it at the level this file gives its
// rule.
//
// Assigning the severity rather than leaving it to FormatAlert is what makes a
// per-format level take effect: a check builds its alerts with the level it was
// compiled with already set, and FormatAlert only fills a severity that is
// still empty. See #965.
func setLevel(a *core.Alert, f *core.File, info check.Definition, name string) {
	level := f.Level(name, info.Level)

	core.FormatAlert(a, info.Limit, level, name)
	a.Severity = level
}

// lintBlockSerial is lintBlock without the concurrency, and without what it
// costs to set up.
func (l *Linter) lintBlockSerial(f *core.File, blk nlp.Block, rules []scopedRule, lines, pad int, lookup bool) error {
	for _, r := range rules {
		name, chk := r.name, r.rule
		if !l.shouldRun(name, f, chk) {
			continue
		}

		info := chk.Fields()

		var start time.Time
		if l.RuleHook != nil {
			start = time.Now()
		}
		alerts, err := chk.Run(blockFor(blk, chk), f, l.Manager.Config)
		if l.RuleHook != nil {
			l.RuleHook(name, time.Since(start))
		}
		if err != nil {
			return err
		}
		for i := range alerts {
			if f.QueryComments(name + "[" + alerts[i].Match + "]") {
				continue
			}
			setLevel(&alerts[i], f, info, name)
			f.AddAlert(alerts[i], blk, lines, pad, lookup)
		}
	}

	return nil
}

// runConcurrently fills found[i] for each rule named in todo.
//
// An empty result is stored as a non-nil slice so the serial pass can tell a
// rule that ran and found nothing from one it has yet to run.
func (l *Linter) runConcurrently(f *core.File, blk nlp.Block, rules []scopedRule, todo []int, found [][]core.Alert) error {
	if len(todo) < 2 {
		return nil
	}

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		failed error
	)

	for _, i := range todo {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			blockWorkers <- struct{}{}
			defer func() { <-blockWorkers }()

			alerts, err := rules[i].rule.Run(blockFor(blk, rules[i].rule), f, l.Manager.Config)
			if alerts == nil {
				alerts = []core.Alert{}
			}

			mu.Lock()
			defer mu.Unlock()
			if err != nil && failed == nil {
				failed = err
			}
			found[i] = alerts
		}(i)
	}

	wg.Wait()

	return failed
}

// lookup reads a setting given for a rule, falling back to one given for the
// style it belongs to.
func lookup(settings map[string]bool, rule, style string) (bool, bool) {
	if val, ok := settings[rule]; ok {
		return val, true
	}
	val, ok := settings[style]
	return val, ok
}

// lookupUnless is lookup, skipping a key that is marked unset.
func lookupUnless(settings, unset map[string]bool, rule, style string) (bool, bool) {
	if val, ok := settings[rule]; ok && !unset[rule] {
		return val, true
	}
	if val, ok := settings[style]; ok && !unset[style] {
		return val, true
	}
	return false, false
}

// inScopeFor returns the rules that could run on blk, by scope alone.
//
// Built once per distinct block scope and reused. Everything else shouldRun
// weighs -- in-text comments, the file's own settings, the minimum level --
// varies per file and is still decided there.
// blockFor is blk as the rule sees it: with the text of any inline element
// the rule's scope negates blanked out.
func blockFor(blk nlp.Block, chk check.Rule) nlp.Block {
	if len(blk.Inline) == 0 {
		return blk
	}
	excluded := check.NewScope(chk.Fields().Scope).Excluded
	if len(excluded) == 0 {
		return blk
	}
	return blk.Without(excluded)
}

func (l *Linter) inScopeFor(blk nlp.Block) []scopedRule {
	key := blk.Scope + "\x00" + blk.Parent
	if l.inScope != nil {
		if hit, ok := l.inScope.Load(key); ok {
			return hit.([]scopedRule) //nolint:errcheck // only []scopedRule is stored
		}
	}

	rules := l.Manager.Rules()
	found := make([]scopedRule, 0, len(rules))
	for name, chk := range rules {
		if check.NewScope(chk.Fields().Scope).Matches(blk) {
			found = append(found, scopedRule{name: name, rule: chk})
		}
	}

	// A stable order, so that two blocks of the same scope are linted in the
	// same sequence rather than in whatever order the map produced.
	sort.Slice(found, func(i, j int) bool { return found[i].name < found[j].name })

	if l.inScope != nil {
		l.inScope.Store(key, found)
	}

	return found
}

// isStylesPath reports whether dir is one of the configuration's StylesPaths.
func (l *Linter) isStylesPath(dir string) bool {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return false
	}
	for _, p := range l.Manager.Config.Paths {
		if candidate, absErr := filepath.Abs(p); absErr == nil && candidate == abs {
			return true
		}
	}
	return false
}

// runsScoped reports whether a rule scoped to `scope` will run on f.
func (l *Linter) runsScoped(f *core.File, scope string) bool {
	rules := l.Manager.Rules()
	for _, name := range l.Manager.RulesForScope(scope) {
		// A `--filter` removes rules after they were registered.
		if chk, ok := rules[name]; ok && l.shouldRun(name, f, chk) {
			return true
		}
	}
	return false
}

func (l *Linter) shouldRun(name string, f *core.File, chk check.Rule) bool {
	minLevel := l.Manager.Config.MinAlertLevel
	run := false

	details := chk.Fields()

	// Configuration addresses the defining rule: a `consistency` alert's
	// name carries a matched term, and a rule's own name may span
	// subdirectories, so the rule is found by name, not by dot-count.
	// See #129.
	name = l.Manager.RuleForAlert(name)

	if f.QueryComments(name) {
		// It has been disabled via an in-text comment.
		return false
	} else if core.LevelToInt[f.Level(name, details.Level)] < minLevel {
		// The level this file gives the rule, which a section may have changed
		// for this format alone. See #965.
		return false
	}

	style := core.StyleName(name)

	// Has the check been disabled for this extension?
	//
	// The rule's own setting is looked for first and the style's only after,
	// so that `proselint = NO` can turn a style off while
	// `proselint.Typography = YES` keeps one of its rules.
	if val, ok := lookup(f.Checks, name, style); ok && !run {
		if !val {
			return false
		}
		run = true
	}

	// Has the check been disabled for all extensions? A key the section
	// marked UNSET takes no global setting either.
	if val, ok := lookupUnless(l.Manager.Config.GChecks, f.Unset, name, style); ok && !run {
		if !val {
			return false
		}
		run = true
	}

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
	ref := filepath.ToSlash(core.NormalizePath(old, l.Manager.Config.Formats))

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

// stopExternal shuts down the helper processes a run started.
func (l *Linter) stopExternal() {
	if l.adoc != nil {
		l.adoc.stop()
		l.adoc = nil
		l.adocOnce = sync.Once{}
	}
	if l.typst != nil {
		l.typst.stop()
		l.typst = nil
		l.typstPoolOnce = sync.Once{}
	}
	if l.rst != nil {
		l.rst.stop()
		l.rst = nil
		l.rstOnce = sync.Once{}
	}
	l.singleDoc = false
}
