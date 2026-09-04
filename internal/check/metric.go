package check

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/d5/tengo/v2"
	"github.com/d5/tengo/v2/parser"
	"github.com/d5/tengo/v2/stdlib"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/nlp"
)

var boilerplate = `math := import("math"); __res__ := (%s)`
var variables = []string{
	"pre", "list", "blockquote", "heading_h1", "heading_h2", "heading_h3",
	"heading_h4", "heading_h5", "heading_h6"}
var headings = regexp.MustCompile(`heading\.(h[1-6])`)

// Metric implements arbitrary, readability-like formulas.
type Metric struct {
	Definition `mapstructure:",squash"`
	// `metric` (`string`): the formula to be dynamically evaluated.
	//
	// Variables: # of words, # of sentences, etc.
	Formula   string
	Condition string

	path string
}

// NewMetric creates a new `metric`-based rule.
func NewMetric(_ *core.Config, generic baseCheck, path string) (Metric, error) {
	rule := Metric{}

	err := decodeRule(generic, &rule)
	if err != nil {
		return rule, readStructureError(err, path)
	}

	rule.path = path
	rule.Definition.Scope = measuredScope(rule.Definition.Scope)
	rule.Formula = headings.ReplaceAllString(rule.Formula, "heading_$1")

	return rule, nil
}

// measuredScope is the scope a measuring rule runs on.
//
// Unset, it measures the document. So does `text`: the document's prose is
// what the summary holds, and `text` was what an unset scope used to compile
// to, so a rule that declared it keeps measuring what it always measured.
// Any other scope measures the blocks it names.
func measuredScope(declared []string) []string {
	if len(declared) == 0 || (len(declared) == 1 && declared[0] == "text") {
		return []string{"summary"}
	}
	return declared
}

// Run calculates the readability level of the given text.
func (o Metric) Run(blk nlp.Block, _ *core.File, _ *core.Config) ([]core.Alert, error) {
	alerts := []core.Alert{}

	// A formula is compiled and run as a Tengo program, so it needs the same
	// deadline a script rule gets; see tengoTimeout. Both evalMath calls share
	// it, which bounds the rule as a whole rather than each half of it.
	ctx, cancel := context.WithTimeout(context.Background(), tengoTimeout)
	defer cancel()

	parameters := core.BlockMetrics(blk.Text, blk.Metrics)
	if len(parameters) == 0 {
		// empty file.
		return alerts, nil
	}

	for _, k := range variables {
		if _, ok := parameters[k]; !ok {
			// If a variable wasn't found in the given file, we set its value
			// to 0.0.
			parameters[k] = 0.0
		}
	}

	// The actual result of our formula.
	//
	// We need this to allow showing the result in a rule's message.
	res, err := evalMath(ctx, o.Formula, parameters)
	if err != nil {
		return alerts, ruleError(
			o.Name, "formula", o.path, err, errors.Is(ctx.Err(), context.DeadlineExceeded))
	}

	// The binary result of our formula:
	eqb := fmt.Sprintf("%f %s", res, o.Condition)

	match, err := evalMath(ctx, eqb, parameters)
	if err != nil {
		return alerts, ruleError(
			o.Name, "condition", o.path, err, errors.Is(ctx.Err(), context.DeadlineExceeded))
	}

	if match.(bool) {
		a := core.Alert{Check: o.Name, Severity: o.Level, Span: []int{1, 1},
			Link: o.Link}
		a.Message, a.Description = formatMessages(
			o.Message, o.Description, fmt.Sprintf("%.2f", res))
		alerts = append(alerts, a)
	}

	return alerts, nil
}

// Fields provides access to the internal rule definition.
func (o Metric) Fields() Definition {
	return o.Definition
}

// Pattern is the internal regex pattern used by this rule.
func (o Metric) Pattern() string {
	return o.Formula
}

// checkExpression rejects anything that is not a single expression.
//
// A formula is placed into the boilerplate above by substitution, so a value
// carrying its own parentheses could parse as several statements rather than
// the one it is meant to be. `condition` is substituted the same way, after the
// computed value, and needs the same check.
//
// Parsing the value on its own settles what it is while it is still a string:
// anything that is not exactly one expression is a formula this rule cannot
// evaluate, and saying so here gives a better error than compiling it would.
func checkExpression(expr string) error {
	fileSet := parser.NewFileSet()
	srcFile := fileSet.AddFile("expression", -1, len(expr))

	parsed, err := parser.NewParser(srcFile, []byte(expr), nil).ParseFile()
	if err != nil {
		return fmt.Errorf("invalid expression %q: %w", expr, err)
	}

	if len(parsed.Stmts) != 1 {
		return fmt.Errorf(
			"expected a single expression, found %d statements in %q",
			len(parsed.Stmts), expr)
	}
	if _, ok := parsed.Stmts[0].(*parser.ExprStmt); !ok {
		return fmt.Errorf(
			"expected an expression, found %T in %q", parsed.Stmts[0], expr)
	}

	return nil
}

func evalMath(
	ctx context.Context,
	expr string,
	params map[string]interface{},
) (interface{}, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("empty expression")
	}

	if err := checkExpression(expr); err != nil {
		return nil, err
	}

	script := tengo.NewScript([]byte(fmt.Sprintf(boilerplate, expr)))
	script.SetImports(stdlib.GetModuleMap("math"))

	for pk, pv := range params {
		err := script.Add(pk, pv)
		if err != nil {
			return nil, fmt.Errorf("script add: %w", err)
		}
	}

	compiled, err := script.RunContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("script run: %w", err)
	}

	return compiled.Get("__res__").Value(), nil
}
