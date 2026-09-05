package check

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/d5/tengo/v2"
	"github.com/d5/tengo/v2/stdlib"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

// tengoTimeout bounds one execution of a rule's program, for both the `script`
// rules below and the `metric` formulas beside them.
//
// A rule that loops without a way out would otherwise stop Vale with no output
// and no error, which reads as the tool having crashed rather than as one rule
// needing attention. Ending the run reports it instead.
//
// Generous on purpose: a script matches against a single block and a formula
// evaluates once per file, so this sits well above what a working rule needs.
const tengoTimeout = 2 * time.Second

// ruleError reports a rule's runtime failure against the rule itself.
//
// The path alone identifies the file but not which check inside it stopped, and
// a package may define several. The name is what appears in a rule's output and
// in a user's config, so it is the handle they already have for switching the
// thing off.
//
// A deadline is also restated: `context deadline exceeded` is Go's wording for
// an internal mechanism, and a style author reading it has no reason to connect
// it to a rule of theirs that never returns.
func ruleError(name, field, path string, err error, timedOut bool) error {
	msg := err.Error()
	if timedOut {
		msg = fmt.Sprintf("did not finish within %s", tengoTimeout)
	}
	if name != "" {
		msg = name + ": " + msg
	}

	return core.NewE201FromTarget(msg, field, path)
}

// Script is Tango-based script.
//
// see https://github.com/d5/tengo.
type Script struct {
	Definition `mapstructure:",squash"`
	Script     string

	// compiled is the parsed program, built once with the rule.
	//
	// Compiling is the expensive part of running a script, and the source
	// never changes; only the text handed to it does. Each block gets a clone,
	// which copies the globals and leaves the bytecode shared.
	compiled *tengo.Compiled

	path string
}

// NewScript creates a new `script`-based rule.
func NewScript(cfg *core.Config, generic baseCheck, path string) (Script, error) {
	rule := Script{}

	err := decodeRule(generic, &rule)
	if err != nil {
		return rule, readStructureError(err, path)
	}

	if strings.HasSuffix(rule.Script, ".tengo") {
		file := core.FindConfigAsset(cfg, rule.Script, core.ScriptDir)

		b, scriptErr := os.ReadFile(file)
		if scriptErr != nil {
			return rule, core.NewE201FromTarget(
				scriptErr.Error(), rule.Script, path)
		}

		rule.Script = string(b)
	}

	rule.path = path

	program := tengo.NewScript([]byte(rule.Script))
	// NOTE: We don't want to enable the`os` module because of the security
	// implications?
	//
	// See #495, for example.
	program.SetImports(stdlib.GetModuleMap("text", "fmt", "math"))

	// Declared now so the compiled program has a slot for it; the value is
	// replaced per block.
	if err = program.Add("scope", ""); err != nil {
		return rule, core.NewE201FromTarget(err.Error(), "script", path)
	}

	compiled, err := program.Compile()
	if err != nil {
		return rule, core.NewE201FromTarget(err.Error(), "script", path)
	}
	rule.compiled = compiled

	return rule, nil
}

// Run executes the given script and returns its Alerts.
func (s Script) Run(blk nlp.Block, _ *core.File, cfg *core.Config) ([]core.Alert, error) {
	var alerts []core.Alert

	// A clone per block: the program is shared, its globals are not, and Vale
	// lints files concurrently.
	compiled := s.compiled.Clone()

	if err := compiled.Set("scope", blk.Text); err != nil {
		return alerts, core.NewE201FromTarget(err.Error(), "script", s.path)
	}

	ctx, cancel := context.WithTimeout(context.Background(), tengoTimeout)
	defer cancel()

	if err := compiled.RunContext(ctx); err != nil {
		return alerts, ruleError(
			s.Name, "script", s.path, err, errors.Is(ctx.Err(), context.DeadlineExceeded))
	}

	for _, match := range parseMatches(compiled.Get("matches").Array()) {
		matchText := blk.Text[match["begin"].(int):match["end"].(int)]
		matchLoc := []int{match["begin"].(int), match["end"].(int)}
		// NOTE: We can't call `makeAlert` here because `script`-based rules
		// don't use our custom regexp2 library, which means the offsets
		// (`re2loc`) will be off.
		a := core.Alert{
			Check:          s.Name,
			Severity:       s.Level,
			Span:           matchLoc,
			Link:           s.Link,
			Match:          matchText,
			Action:         s.Action,
			HasByteOffsets: true}

		if matchMsg, ok := match["message"].(string); ok {
			a.Message, a.Description = formatMessages(matchMsg, s.Description, matchText)
		} else {
			a.Message, a.Description = formatMessages(s.Message, s.Description, matchText)
		}

		if err := resolveFix(&a, cfg); err != nil {
			return alerts, err
		}
		alerts = append(alerts, a)
	}

	return alerts, nil
}

func parseMatches(a []interface{}) []map[string]interface{} {
	matches := []map[string]interface{}{}
	for _, i := range a {
		m, _ := i.(map[string]interface{})
		match := map[string]interface{}{
			"begin": int(m["begin"].(int64)),
			"end":   int(m["end"].(int64)),
		}

		if msg, ok := m["message"].(string); ok {
			match["message"] = msg
		}

		matches = append(matches, match)
	}
	return matches
}

// Fields provides access to the internal rule definition.
func (s Script) Fields() Definition {
	return s.Definition
}

// Pattern is the internal regex pattern used by this rule.
func (s Script) Pattern() string {
	return s.Script
}
