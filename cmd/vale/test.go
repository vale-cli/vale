package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pterm/pterm"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/testsuite"
)

// errTestFailed reports cases that ran and did not pass.
//
// It is not an error in the sense the others are -- Vale worked, the
// configuration did not -- so main exits 1 with it rather than printing it as
// a failure of Vale's own.
var errTestFailed = errors.New("test cases failed")

// testReport is the JSON form of a run.
type testReport struct {
	Passed  int          `json:"passed"`
	Failed  int          `json:"failed"`
	Results []testResult `json:"results"`
}

type testResult struct {
	File   string `json:"file"`
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Reason string `json:"reason,omitempty"`
	Got    string `json:"got,omitempty"`
	Want   string `json:"want,omitempty"`
}

// runTests runs the test cases in the given files or directories.
func runTests(args []string, flags *core.CLIFlags) error {
	paths, err := testsuite.Find(args)
	if err != nil {
		return core.NewE100("test", err)
	} else if len(paths) == 0 {
		return core.NewE100("test", errors.New("no test files found"))
	}

	runner := testsuite.NewRunner(flags)

	// Find casts a wide net -- any YAML file might be a rule with in-source
	// cases -- so the files worth reporting are the ones that held cases, not
	// the ones considered.
	files := 0

	var results []testsuite.Result
	for _, path := range paths {
		cases, lErr := testsuite.Load(path)
		if lErr != nil {
			return core.NewE100("test", lErr)
		}
		if len(cases) > 0 {
			files++
		}

		for _, c := range cases {
			results = append(results, runner.Run(c))
		}
	}

	if len(results) == 0 {
		return core.NewE100("test", errors.New("no test cases found"))
	}

	// A case that could not be run at all is a broken configuration, not a
	// failing assertion: report it as Vale's own error and stop.
	for _, r := range results {
		if r.Err != nil {
			return core.NewE100(filepath.Base(r.Case.Path), r.Err)
		}
	}

	if flags.Output == "JSON" {
		return reportTestsJSON(results)
	}

	return reportTests(results, files)
}

func reportTests(results []testsuite.Result, files int) error {
	failed := 0

	for _, r := range results {
		if !r.Failed() {
			continue
		}
		failed++

		fmt.Printf("\n%s %s %s\n\n",
			pterm.Red("✗"),
			pterm.Bold.Sprint(r.Case.Name),
			pterm.Gray("— "+r.Reason))

		if r.Case.About != "" {
			fmt.Printf("  %s %s\n", pterm.Gray("about"), r.Case.About)
		}
		fmt.Printf("  %s  %s\n\n", pterm.Gray("from"), relPath(r.Case.Path))

		if r.Case.Want != nil {
			fmt.Print(renderDiff(testsuite.Diff(*r.Case.Want, r.Got)))
		} else {
			fmt.Print(indentBlock(blockOrNone(r.Got)))
		}
	}

	summary := fmt.Sprintf("%d %s — %d passed, %d failed",
		files, pluralize("file", files), len(results)-failed, failed)

	if failed > 0 {
		fmt.Println()
		pterm.Error.Println(summary)
		return errTestFailed
	}

	pterm.Success.Println(summary)
	return nil
}

func reportTestsJSON(results []testsuite.Result) error {
	report := testReport{Results: make([]testResult, 0, len(results))}

	for _, r := range results {
		if r.Failed() {
			report.Failed++
		} else {
			report.Passed++
		}

		out := testResult{
			File:   r.Case.Path,
			Name:   r.Case.Name,
			Passed: !r.Failed(),
			Reason: r.Reason,
			Got:    r.Got,
		}
		if r.Case.Want != nil {
			out.Want = *r.Case.Want
		}

		report.Results = append(report.Results, out)
	}

	if err := printJSON(report); err != nil {
		return err
	}
	if report.Failed > 0 {
		return errTestFailed
	}

	return nil
}

// renderDiff colors a diff and indents it under its heading.
func renderDiff(diff []testsuite.Line) string {
	if len(diff) == 0 {
		return indentBlock("(no alerts)")
	}

	var b strings.Builder
	for _, line := range diff {
		text := fmt.Sprintf("%c %s", line.Op, line.Text)

		switch line.Op {
		case testsuite.Del:
			text = pterm.Red(text)
		case testsuite.Add:
			text = pterm.Green(text)
		case testsuite.Same:
			text = pterm.Gray(text)
		}

		fmt.Fprintf(&b, "    %s\n", text)
	}

	fmt.Fprintf(&b, "\n    %s   %s\n",
		pterm.Red("- expected"), pterm.Green("+ actual"))

	return b.String()
}

// relPath shortens a path against the working directory, since that is where
// the reader is standing.
func relPath(path string) string {
	wd, err := os.Getwd()
	if err != nil {
		return path
	}

	rel, err := filepath.Rel(wd, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}

	return rel
}

// indentBlock indents a rendered block so it sits under its heading.
func indentBlock(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		fmt.Fprintf(&b, "    %s\n", line)
	}
	return b.String()
}

func blockOrNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(no alerts)"
	}
	return s
}
