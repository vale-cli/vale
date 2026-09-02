package main

import (
	"fmt"

	"github.com/vale-cli/vale/v3/internal/check"
	"github.com/vale-cli/vale/v3/internal/core"
)

// PrintJSONAlerts prints Alerts in map[file.path][]Alert form.
func PrintJSONAlerts(linted []*core.File) bool {
	alertCount := 0
	formatted := map[string][]core.Alert{}
	for _, f := range linted {
		for _, a := range f.SortedAlerts() {
			if a.Severity == "error" {
				alertCount++
			}
			formatted[f.Path] = append(formatted[f.Path], a)
		}
	}
	fmt.Println(getJSON(formatted))
	return alertCount != 0
}

// countAlerts tallies this run's alerts per enabled check, seeding every
// check the manager loaded with zero. Runtime names (a consistency rule's
// per-pair entries) collapse onto the rule that produced them.
func countAlerts(mgr *check.Manager, linted []*core.File) map[string]int {
	counts := map[string]int{}
	for name := range mgr.Rules() {
		counts[name] = 0
	}
	for _, f := range linted {
		for _, a := range f.Alerts {
			name := a.Check
			if _, known := counts[name]; !known {
				name = mgr.RuleForAlert(name)
			}
			counts[name]++
		}
	}
	return counts
}

// jsonWithCounts is the opt-in `--counts` shape: the usual alert map under
// `files`, plus one entry per enabled check under `counts`. Zeros are the
// point -- an enabled check that never fires is otherwise invisible, and a
// dead rule is indistinguishable from a working one.
type jsonWithCounts struct {
	Files  map[string][]core.Alert `json:"files"`
	Counts map[string]int          `json:"counts"`
}

// PrintJSONAlertsWithCounts prints alerts alongside per-check counts. The
// counts reflect the alerts in this output: a check silenced by the alert
// level still reads 0, which keeps the two halves of the report consistent.
func PrintJSONAlertsWithCounts(linted []*core.File, counts map[string]int) bool {
	alertCount := 0
	out := jsonWithCounts{Files: map[string][]core.Alert{}, Counts: counts}
	for _, f := range linted {
		for _, a := range f.SortedAlerts() {
			if a.Severity == "error" {
				alertCount++
			}
			out.Files[f.Path] = append(out.Files[f.Path], a)
		}
	}
	fmt.Println(getJSON(out))
	return alertCount != 0
}
