package check

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"

	"golang.org/x/exp/maps"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
	"github.com/vale-cli/vale/v3/internal/system"
)

// Manager controls the loading and validating of the check extension points.
type Manager struct {
	Config *core.Config

	scopes       map[string]struct{}
	rules        map[string]Rule
	styles       []string
	needsTagging bool
}

// NewManager creates a new Manager and loads the rule definitions (that is,
// extended checks) specified by configuration.
func NewManager(config *core.Config) (*Manager, error) {
	var path string

	mgr := Manager{
		Config: config,

		rules:  make(map[string]Rule),
		scopes: make(map[string]struct{}),
	}

	// TODO: Should we only load these if we're using them?
	err := mgr.loadDefaultRules()
	if err != nil {
		return &mgr, err
	}

	// Load our styles ...
	err = mgr.loadStyles(mgr.Config.Styles)
	if err != nil {
		return &mgr, err
	}

	for _, chk := range mgr.Config.Checks {
		// Load any remaining individual rules.
		if !strings.Contains(chk, ".") {
			// A rule must be associated with a style (i.e., "Style[.]Rule").
			continue
		}
		parts := strings.Split(chk, ".")
		if !mgr.hasStyle(parts[0]) {
			// If this rule isn't part of an already-loaded style, we load it
			// individually. Every segment after the style is a path
			// component: `Std.dates.TimeFormat` is `Std/dates/TimeFormat.yml`.
			fName := filepath.Join(parts[1:]...) + ".yml"
			for _, p := range mgr.Config.SearchPaths() {
				path = filepath.Join(p, parts[0], fName)
				if !system.FileExists(path) {
					continue
				}
				if err = mgr.addCheckFile(chk, path); err != nil {
					return &mgr, err
				}
			}
		}
	}

	mgr.rules, err = filter(&mgr)
	return &mgr, err
}

// AddRule adds the given rule to the manager.
func (mgr *Manager) AddRule(name string, rule Rule) error {
	if _, found := mgr.rules[name]; !found {
		mgr.rules[name] = rule
		return nil
	}
	return fmt.Errorf("the rule '%s' has already been added", name)
}

// RuleForAlert maps an alert's check name back to the rule that defines it.
//
// Most alerts carry their rule's name already. A `consistency` rule names its
// alerts `Style.Rule.<term>`, and a rule may itself sit in a subdirectory
// (`Std.dates.TimeFormat`), so neither dot-counting nor position says where
// the rule ends: the longest known prefix does. An unknown name falls back to
// its first two segments, which is the historical reading (see #129).
func (mgr *Manager) RuleForAlert(name string) string {
	if _, ok := mgr.rules[name]; ok {
		return name
	}

	for prefix := name; strings.Contains(prefix, "."); {
		prefix = prefix[:strings.LastIndex(prefix, ".")]
		if _, ok := mgr.rules[prefix]; ok {
			return prefix
		}
	}

	if parts := strings.Split(name, "."); len(parts) > 2 {
		return parts[0] + "." + parts[1]
	}
	return name
}

// AddRuleFromFile adds the given rule to the manager.
func (mgr *Manager) AddRuleFromFile(name, path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return core.NewE100("ReadFile", err)
	}
	return mgr.addCheck(content, name, path)
}

// Rules are all of the Manager's compiled `Rule`s.
func (mgr *Manager) Rules() map[string]Rule {
	return mgr.rules
}

// HasScope returns `true` if the manager has a rule that applies to `scope`.
func (mgr *Manager) HasScope(scope string) bool {
	_, found := mgr.scopes[scope]
	return found
}

// NeedsTagging indicates if POS tagging is needed.
func (mgr *Manager) NeedsTagging() bool {
	return mgr.needsTagging
}

// AssignNLP determines what NLP tasks a file needs.
func (mgr *Manager) AssignNLP(f *core.File) nlp.Info {
	return nlp.Info{
		Scope:        f.RealExt,
		Segmentation: mgr.HasScope("sentence"),
		Splitting:    mgr.HasScope("paragraph"),
		Tagging:      mgr.NeedsTagging(),
		Endpoint:     f.NLP.Endpoint,
		Lang:         f.NLP.Lang,
	}
}

// compileGCPercent is the collector target held during rule compilation. It
// costs about 40 MB of peak heap on a 550-rule style.
const compileGCPercent = 800

// maxCompileWorkers is where compiling stops going faster. Past it the workers
// contend for the allocator rather than the CPU, and a ninth adds processor
// time without taking wall clock off.
const maxCompileWorkers = 4

func compileWorkers() int {
	return min(maxCompileWorkers, runtime.GOMAXPROCS(0))
}

func (mgr *Manager) addStyle(path string) error {
	// Compiling a rule is the expensive half of loading one -- parsing the
	// YAML and, mostly, handing its patterns to the regular-expression engine,
	// which for a case-insensitive pattern enumerates Unicode case folds. Done
	// one rule at a time that is most of what Vale spends before it reads a
	// byte of input, and the rules are independent of each other.
	//
	// So they are compiled in parallel and registered afterwards, in the order
	// the walk found them. Registration touches shared state and stays serial;
	// keeping it ordered means the rule that wins a name clash, and the error
	// that gets reported first, do not depend on which goroutine finished
	// first.
	type source struct {
		name, path string
	}

	var sources []source
	err := system.Walk(path, func(fp string, info fs.FileInfo, err error) error {
		switch {
		case err != nil:
			return err
		case info.IsDir() && fp != path &&
			(strings.HasPrefix(info.Name(), ".") || strings.HasPrefix(info.Name(), "_")):
			// A subdirectory joins its rules' names, which means YAML parked
			// under a style -- drafts, retired rules -- now loads. A dot or
			// underscore prefix keeps a directory inert, the same convention
			// the Go toolchain and the test-file walk use.
			return filepath.SkipDir
		case info.IsDir() || !strings.HasSuffix(info.Name(), ".yml"):
			return nil
		case core.IsTestFile(info.Name()):
			// A rule's cases live beside it, so the style directory holds YAML
			// that is not a rule. Loaded as one it fails on `extends`, and the
			// whole configuration stops. See #1122.
			return nil
		}

		chkName, nErr := core.CheckName(path, fp)
		if nErr != nil {
			return nErr
		}

		sources = append(sources, source{name: chkName, path: fp})
		return nil
	})
	if err != nil {
		return err
	}

	type result struct {
		chkName   string
		rule      Rule
		taggedPOS bool
		err       error
		skip      bool
	}

	results := make([]result, len(sources))

	// Compiling allocates hard enough that the collector, not the CPU, decides
	// how fast this goes: at the default target it is a third of the phase's
	// processor time and holds the speedup to under two cores of eight. The
	// burst is bounded, so the target is raised for it and restored after.
	defer debug.SetGCPercent(debug.SetGCPercent(compileGCPercent))

	var wg sync.WaitGroup
	sem := make(chan struct{}, compileWorkers())

	for i, src := range sources {
		chkName := src.name

		// A rule already loaded under this name is not re-read: the first
		// search path to define it wins, as before.
		if _, ok := mgr.rules[chkName]; ok {
			results[i] = result{skip: true}
			continue
		}

		// Nor is one the configuration can never run: `Style.Rule = NO` is
		// applied in lint.shouldRun, long after the pattern reached the engine.
		if !mgr.enabledSomewhere(chkName) {
			results[i] = result{skip: true}
			continue
		}
		results[i].chkName = chkName

		wg.Add(1)
		go func(i int, path string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			data, rerr := os.ReadFile(path)
			if rerr != nil {
				results[i].err = core.NewE201FromPosition(rerr.Error(), path, 1)
				return
			}
			results[i].rule, results[i].taggedPOS, results[i].err = mgr.compileCheck(
				data, results[i].chkName, path)
		}(i, src.path)
	}
	wg.Wait()

	for i := range results {
		if results[i].skip {
			continue
		}
		if results[i].err != nil {
			return results[i].err
		}
		if rerr := mgr.registerCheck(
			results[i].chkName, results[i].rule, results[i].taggedPOS); rerr != nil {
			return rerr
		}
	}

	return nil
}

func (mgr *Manager) addCheckFile(chkName, path string) error {
	f, err := os.ReadFile(path)
	if err != nil {
		return core.NewE201FromPosition(err.Error(), path, 1)
	}

	if _, ok := mgr.rules[chkName]; !ok {
		return mgr.addCheck(f, chkName, path)
	}
	return nil
}

func (mgr *Manager) addCheck(file []byte, chkName, path string) error {
	rule, taggedPOS, err := mgr.compileCheck(file, chkName, path)
	if err != nil {
		return err
	}
	return mgr.registerCheck(chkName, rule, taggedPOS)
}

// compileCheck turns a rule's source into a Rule.
//
// It reads mgr.Config but does not touch mgr's mutable state, so it is safe to
// run concurrently for different rules. Everything that writes to the Manager
// is in registerCheck.
func (mgr *Manager) compileCheck(file []byte, chkName, path string) (Rule, bool, error) {
	// Load the rule definition.
	generic, err := parse(file, path)
	if err != nil {
		return nil, false, err
	}

	// Set default values, if necessary.
	generic["name"] = chkName
	generic["path"] = path

	// A level set for the rule wins; a level set for its style covers the rest
	// of that style, so `proselint = suggestion` can be written once and
	// `proselint.Typography = warning` kept alongside it.
	if level, ok := mgr.Config.RuleToLevel[chkName]; ok {
		generic["level"] = level
	} else if level, ok = mgr.Config.RuleToLevel[core.StyleName(chkName)]; ok {
		generic["level"] = level
	} else if _, ok = generic["level"]; !ok {
		generic["level"] = "warning"
	}
	if scope, ok := generic["scope"]; scope == nil || !ok {
		// Not for `sequence`, which needs to tell an unset scope from an
		// explicit `text` one: it runs on sentences, and has to know whether
		// the author asked for somewhere in particular to take them from.
		if extends, _ := generic["extends"].(string); extends != "sequence" {
			generic["scope"] = []string{"text"}
		}
	}

	rule, err := buildRule(mgr.Config, generic)
	if err != nil {
		return nil, false, err
	}

	pos, ok := generic["pos"]
	return rule, ok && pos != "", nil
}

// scopeBases names the block families a declared scope needs built.
//
// A scope may chain terms with `&`, and each term asks for its own family:
// `paragraph & ~heading` needs paragraph splitting as much as `paragraph`
// does. Reading the whole chain as one name left HasScope false, splitting
// off, and the rule silently matching nothing. See #1133.
//
// A negated term asks for a family's absence, which needs nothing built.
func scopeBases(s string) []string {
	bases := []string{}
	for _, part := range strings.Split(s, "&") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "~") {
			continue
		}
		bases = append(bases, strings.Split(part, ".")[0])
	}
	return bases
}

// registerCheck records a compiled rule and what it implies for the run.
func (mgr *Manager) registerCheck(chkName string, rule Rule, taggedPOS bool) error {
	for _, s := range rule.Fields().Scope {
		for _, base := range scopeBases(s) {
			mgr.scopes[base] = struct{}{}
		}
	}

	if rule.Fields().Extends == "sequence" || taggedPOS {
		mgr.needsTagging = true
	}

	return mgr.AddRule(chkName, rule)
}

func (mgr *Manager) loadDefaultRules() error {
	if !mgr.needsStyle("Vale") {
		return nil
	}

	for _, style := range defaultStyles {
		if core.StringInSlice(style, mgr.styles) {
			return fmt.Errorf("'%v' collides with built-in style", style)
		}
	}

	repetition := defaultRules["Repetition"]
	if level, ok := mgr.Config.RuleToLevel["Vale.Repetition"]; ok {
		repetition["level"] = level
	}
	repetition["path"] = "internal"

	rule, err := buildRule(mgr.Config, repetition)
	if err != nil {
		return err
	}
	mgr.rules["Vale.Repetition"] = rule

	spelling := defaultRules["Spelling"]
	if level, ok := mgr.Config.RuleToLevel["Vale.Spelling"]; ok {
		spelling["level"] = level
	}
	spelling["path"] = "internal"

	rule, err = buildRule(mgr.Config, spelling)
	if err != nil {
		return err
	}
	mgr.rules["Vale.Spelling"] = rule

	// TODO: where should this go?
	mgr.loadVocabRules()

	return nil
}

func (mgr *Manager) loadStyles(styles []string) error {
	var found []string
	var need []string

	for _, baseDir := range mgr.Config.SearchPaths() {
		for _, style := range styles {
			p := filepath.Join(baseDir, style)
			if mgr.hasStyle(style) {
				// We've already loaded this style.
				continue
			} else if has := system.IsDir(p); !has {
				need = append(need, style)
				continue
			} else if err := mgr.addStyle(p); err != nil {
				return err
			}
			found = append(found, style)
		}
	}

	for _, s := range need {
		if !core.StringInSlice(s, found) {
			return core.NewE100(
				"loadStyles",
				errors.New("style '"+s+"' does not exist on StylesPath"))
		}
	}

	mgr.styles = append(mgr.styles, found...)
	return nil
}

func (mgr *Manager) loadVocabRules() {
	if len(mgr.Config.AcceptedTokens) > 0 {
		vocab := defaultRules["Terms"]
		for _, term := range mgr.Config.AcceptedTokens {
			vocab["swap"].(map[string]string)[strings.ToLower(term)] = term
		}
		if level, ok := mgr.Config.RuleToLevel["Vale.Terms"]; ok {
			vocab["level"] = level
		}
		rule, _ := buildRule(mgr.Config, vocab)
		mgr.rules["Vale.Terms"] = rule
	}

	if len(mgr.Config.RejectedTokens) > 0 {
		avoid := defaultRules["Avoid"]
		for _, term := range mgr.Config.RejectedTokens {
			avoid["tokens"] = append(avoid["tokens"].([]string), term)
		}
		if level, ok := mgr.Config.RuleToLevel["Vale.Avoid"]; ok {
			avoid["level"] = level
		}
		rule, _ := buildRule(mgr.Config, avoid)
		mgr.rules["Vale.Avoid"] = rule
	}
}

func (mgr *Manager) hasStyle(name string) bool {
	styles := append(mgr.styles, defaultStyles...) //nolint:gocritic
	return core.StringInSlice(name, styles)
}

// enabledSomewhere reports whether any part of the configuration could run the
// named rule, mirroring lint.shouldRun ahead of compiling.
//
// Compiling is the expensive half of loading a rule, and `Style.Rule = NO` was
// applied long after it. The test errs toward compiling, and nothing
// downstream can make a skip wrong: in-text comments only disable, and
// `--filter` selects from what the ini already loaded.
func (mgr *Manager) enabledSomewhere(name string) bool {
	cfg := mgr.Config
	style := core.StyleName(name)

	// Named on: decides wherever it is set, BasedOnStyles or not.
	for _, sec := range cfg.SChecks {
		if val, ok := checkSetting(sec, name, style); ok && val {
			return true
		}
	}
	if val, ok := checkSetting(cfg.GChecks, name, style); ok {
		return val
	}

	// Otherwise it runs where its style is based on, unless that same section
	// switches it off.
	if core.StringInSlice(style, cfg.GBaseStyles) {
		return true
	}
	for sec, styles := range cfg.SBaseStyles {
		if !core.StringInSlice(style, styles) {
			continue
		}
		if val, ok := checkSetting(cfg.SChecks[sec], name, style); ok && !val {
			continue
		}
		return true
	}

	return false
}

// checkSetting reads a rule's setting, falling back to its style's -- the
// precedence lint.lookup uses for the same question at lint time.
func checkSetting(settings map[string]bool, rule, style string) (bool, bool) {
	if val, ok := settings[rule]; ok {
		return val, true
	}
	val, ok := settings[style]
	return val, ok
}

func (mgr *Manager) needsStyle(name string) bool {
	cfg := mgr.Config

	if core.StringInSlice(name, cfg.GBaseStyles) {
		return true
	}

	for _, s := range maps.Keys(cfg.GChecks) {
		if strings.HasPrefix(s, name) {
			return true
		}
	}

	for _, s := range cfg.SBaseStyles {
		if core.StringInSlice(name, s) {
			return true
		}
	}

	for _, s := range cfg.SChecks {
		for _, chk := range maps.Keys(s) {
			if strings.HasPrefix(chk, name) {
				return true
			}
		}
	}

	return false
}
