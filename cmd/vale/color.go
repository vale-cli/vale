package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/olekukonko/tablewriter/tw"
	"github.com/pterm/pterm"

	"github.com/errata-ai/vale/v3/internal/core"
)

// PrintVerboseAlerts prints Alerts in verbose format.
func PrintVerboseAlerts(linted []*core.File, wrap bool) bool {
	var errors, warnings, suggestions int
	var e, w, s int
	var symbol string

	for _, f := range linted {
		e, w, s = printVerboseAlert(f, wrap)
		errors += e
		warnings += w
		suggestions += s
	}

	etotal := fmt.Sprintf("%d %s", errors, pluralize("error", errors))
	wtotal := fmt.Sprintf("%d %s", warnings, pluralize("warning", warnings))
	stotal := fmt.Sprintf("%d %s", suggestions, pluralize("suggestion", suggestions))

	if errors > 0 || warnings > 0 {
		symbol = "\u2716"
	} else {
		symbol = "\u2714"
	}

	n := len(linted)
	if n == 1 && strings.HasPrefix(linted[0].Path, "stdin") {
		fmt.Printf("%s %s, %s and %s in %s.\n", symbol,
			pterm.Red(etotal), pterm.Yellow(wtotal),
			pterm.Blue(stotal), "stdin")
	} else {
		fmt.Printf("%s %s, %s and %s in %d %s.\n", symbol,
			pterm.Red(etotal), pterm.Yellow(wtotal),
			pterm.Blue(stotal), n, pluralize("file", n))
	}

	return errors != 0
}

// printVerboseAlert includes an alert's line, column, level, and message.
func printVerboseAlert(f *core.File, wrap bool) (int, int, int) {
	var loc, level string
	var errors, warnings, notifications int

	alerts := f.SortedAlerts()
	if len(alerts) == 0 {
		return 0, 0, 0
	}

	wrapMode := tw.WrapNone
	if !wrap {
		wrapMode = tw.WrapNormal
	}
	table := newBorderlessTable(os.Stdout, wrapMode)

	fmt.Printf("\n %s", pterm.Underscore.Sprint(f.Path))
	for _, a := range alerts {
		switch a.Severity {
		case "suggestion":
			level = pterm.Blue(a.Severity)
			notifications++
		case "warning":
			level = pterm.Yellow(a.Severity)
			warnings++
		case "error":
			level = pterm.Red(a.Severity)
			errors++
		}
		loc = fmt.Sprintf("%d:%d", a.Line, a.Span[0])
		table.Append([]string{loc, level, a.Message, a.Check})
	}
	fmt.Println()
	table.Render()
	fmt.Println()
	return errors, warnings, notifications
}
