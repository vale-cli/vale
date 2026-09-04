package check

import (
	"fmt"

	"github.com/jdkato/prose/v3/summarize"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

// Readability checks the reading grade level of text.
type Readability struct {
	Definition `mapstructure:",squash"`
	// `metrics` (`array`): One or more of Gunning Fog, Coleman-Liau,
	// Flesch-Kincaid, SMOG, and Automated Readability.
	Metrics []string
	// `grade` (`float`): The highest acceptable score.
	Grade float64
}

// NewReadability creates a new `readability`-based rule.
func NewReadability(_ *core.Config, generic baseCheck, path string) (Readability, error) {
	rule := Readability{}

	err := decodeRule(generic, &rule)
	if err != nil {
		return rule, readStructureError(err, path)
	}

	if core.AllStringsInSlice(rule.Metrics, readabilityMetrics) {
		rule.Definition.Scope = measuredScope(rule.Definition.Scope)
	} else if len(rule.Definition.Scope) == 0 {
		// A rule naming a metric this check doesn't know has always run on
		// `text`, the default an unset scope used to compile to.
		rule.Definition.Scope = []string{"text"}
	}

	return rule, nil
}

// Run calculates the readability level of the given text.
func (o Readability) Run(blk nlp.Block, _ *core.File, _ *core.Config) ([]core.Alert, error) {
	var grade float64
	var alerts []core.Alert

	doc := summarize.NewDocument(blk.Text)

	if core.StringInSlice("SMOG", o.Metrics) {
		grade += doc.SMOG()
	}
	if core.StringInSlice("Gunning Fog", o.Metrics) {
		grade += doc.GunningFog()
	}
	if core.StringInSlice("Coleman-Liau", o.Metrics) {
		grade += doc.ColemanLiau()
	}
	if core.StringInSlice("Flesch-Kincaid", o.Metrics) {
		grade += doc.FleschKincaid()
	}
	if core.StringInSlice("Automated Readability", o.Metrics) {
		grade += doc.AutomatedReadability()
	}

	grade /= float64(len(o.Metrics))
	if grade > o.Grade {
		a := core.Alert{Check: o.Name, Severity: o.Level,
			Span: []int{1, 1}, Link: o.Link}
		a.Message, a.Description = formatMessages(o.Message, o.Description,
			fmt.Sprintf("%.2f", grade))
		alerts = append(alerts, a)
	}

	return alerts, nil
}

// Fields provides access to the internal rule definition.
func (o Readability) Fields() Definition {
	return o.Definition
}

// Pattern is the internal regex pattern used by this rule.
func (o Readability) Pattern() string {
	return ""
}
