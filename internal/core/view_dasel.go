//go:build !vale_noviews

package core

import (
	"context"
	"fmt"
	"regexp"

	"github.com/pelletier/go-toml/v2"
	v2dasel "github.com/tomwright/dasel/v2"
	"github.com/tomwright/dasel/v3"
)

// selectValues returns whatever a selector lands on, one item per hit.
func selectValues(value DaselValue, expr string) ([]any, error) {
	selected, _, err := dasel.Select(context.Background(), value, expr)
	if err != nil {
		// A selector may be written in either dialect, so a failure in both
		// reports both: the user knows which one they meant.
		items, v2err := selectValuesV2(value, expr)
		if v2err != nil {
			return nil, fmt.Errorf("%s (as a v2 selector: %s)",
				trimDaselError(err), trimDaselError(v2err))
		}
		return items, nil
	}

	outer, isSlice := selected.([]any)
	if !isSlice {
		return nil, fmt.Errorf("expected []any, got %T", selected)
	}

	// Unwrap single-element wrapper if present.
	if len(outer) == 1 {
		if inner, isInner := outer[0].([]any); isInner {
			outer = inner
		}
	}
	return outer, nil
}

// daselNoise is the wrapping dasel adds at every level of a selector as an
// error passes up through it.
var daselNoise = regexp.MustCompile(
	`error executing selector: |execution error when processing ast\.\w+: |error executing expression: `)

// trimDaselError strips the per-level wrapping, leaving the cause.
func trimDaselError(err error) string {
	return daselNoise.ReplaceAllString(err.Error(), "")
}

func selectValuesV2(value DaselValue, expr string) ([]any, error) {
	selected, err := v2dasel.Select(value, expr)
	if err != nil {
		return nil, err
	}
	results := make([]any, 0, len(selected))
	for _, v := range selected {
		results = append(results, v.Interface())
	}
	return results, nil
}

// unmarshalTOML decodes a TOML document into v.
func unmarshalTOML(data []byte, v any) error { return toml.Unmarshal(data, v) }
