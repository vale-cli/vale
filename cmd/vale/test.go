package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/pterm/pterm"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/system"
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
	// Uncovered names the rules no case made fire; only with --coverage.
	Uncovered []string `json:"uncovered,omitempty"`
}

type testResult struct {
	File   string `json:"file"`
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Reason string `json:"reason,omitempty"`
	// Error is why the case could not run at all.
	Error string `json:"error,omitempty"`
	Got   string `json:"got,omitempty"`
	Want  string `json:"want,omitempty"`
	// Tags is the input as tagged, for a failing sequence case.
	Tags string `json:"tags,omitempty"`
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
	runner.Install = installPackages
	defer runner.Close()

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

	var uncovered []string
	if flags.Coverage {
		rules, rErr := runner.Rules(args)
		if rErr != nil {
			return core.NewE100("test", rErr)
		}
		uncovered = testsuite.Uncovered(rules, results)
	}

	if flags.Output == "JSON" {
		err = reportTestsJSON(results, uncovered)
	} else {
		err = reportTests(results, files, uncovered)
	}

	// A case that could not run is a broken configuration, not a failing
	// assertion, so it exits 2 -- but only after the rest have reported.
	if n := countBroken(results); n > 0 {
		return core.NewE100("test", fmt.Errorf("%d %s could not run", n, pluralize("case", n)))
	}
	return err
}

// installPackages installs pkgs into styles, as `vale sync` would.
func installPackages(pkgs []string, styles string) error {
	for idx, pkg := range pkgs {
		if err := readPkg(pkg, styles, idx); err != nil {
			return err
		}
	}
	return nil
}

func countBroken(results []testsuite.Result) int {
	n := 0
	for _, r := range results {
		if r.Err != nil {
			n++
		}
	}
	return n
}

func reportTests(results []testsuite.Result, files int, uncovered []string) error {
	failed := 0

	for _, r := range results {
		if !r.Failed() {
			continue
		}
		failed++

		reason := r.Reason
		if r.Err != nil {
			reason = "could not run"
		}
		fmt.Printf("\n%s %s %s\n\n",
			pterm.Red("✗"),
			pterm.Bold.Sprint(r.Case.Name),
			pterm.Gray("— "+reason))

		if r.Case.About != "" {
			fmt.Printf("  %s %s\n", pterm.Gray("about"), r.Case.About)
		}
		fmt.Printf("  %s  %s\n\n", pterm.Gray("from"), relPath(r.Case.Path))

		switch {
		case r.Err != nil:
			fmt.Print(indentBlock(r.Err.Error()))
		case r.Case.Want != nil:
			fmt.Print(renderDiff(testsuite.Diff(*r.Case.Want, r.Got)))
		default:
			fmt.Print(indentBlock(blockOrNone(r.Got)))
		}
		if r.Tags != "" {
			fmt.Printf("\n  %s  %s\n", pterm.Gray("tags"), r.Tags)
		}
	}

	if len(uncovered) > 0 {
		fmt.Printf("\n%s %s\n\n",
			pterm.Red("✗"),
			pterm.Bold.Sprintf("%d %s produced no alert in any case",
				len(uncovered), pluralize("rule", len(uncovered))))
		for _, rule := range uncovered {
			fmt.Printf("    %s\n", rule)
		}
	}

	summary := fmt.Sprintf("%d %s — %d passed, %d failed",
		files, pluralize("file", files), len(results)-failed, failed)
	if len(uncovered) > 0 {
		summary += fmt.Sprintf(", %d uncovered", len(uncovered))
	}

	if failed > 0 || len(uncovered) > 0 {
		fmt.Println()
		pterm.Error.Println(summary)
		return errTestFailed
	}

	pterm.Success.Println(summary)
	return nil
}

func reportTestsJSON(results []testsuite.Result, uncovered []string) error {
	report := testReport{
		Results:   make([]testResult, 0, len(results)),
		Uncovered: uncovered,
	}

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
			Tags:   r.Tags,
		}
		if r.Case.Want != nil {
			out.Want = *r.Case.Want
		}
		if r.Err != nil {
			out.Error = r.Err.Error()
		}

		report.Results = append(report.Results, out)
	}

	if err := printJSON(report); err != nil {
		return err
	}
	if report.Failed > 0 || len(uncovered) > 0 {
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

	rel, _ := system.RelInside(wd, path)
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
