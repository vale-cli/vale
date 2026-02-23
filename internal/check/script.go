package check

import (
	"os"
	"strings"

	"github.com/d5/tengo/v2"
	"github.com/d5/tengo/v2/stdlib"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/nlp"
)

// Script is Tango-based script.
//
// see https://github.com/d5/tengo.
type Script struct {
	Definition `mapstructure:",squash"`
	Script     string

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
	return rule, nil
}

// Run executes the given script and returns its Alerts.
func (s Script) Run(blk nlp.Block, _ *core.File, _ *core.Config) ([]core.Alert, error) {
	var alerts []core.Alert

	script := tengo.NewScript([]byte(s.Script))
	// NOTE: We don't want to enable the`os` module because of the security
	// implications?
	//
	// See #495, for example.
	script.SetImports(stdlib.GetModuleMap("text", "fmt", "math"))

	err := script.Add("scope", blk.Text)
	if err != nil {
		return alerts, core.NewE201FromTarget(err.Error(), "script", s.path)
	}

	compiled, err := script.Compile()
	if err != nil {
		return alerts, core.NewE201FromTarget(err.Error(), "script", s.path)
	}

	if err = compiled.Run(); err != nil {
		return alerts, core.NewE201FromTarget(err.Error(), "script", s.path)
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
