package main

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"github.com/olekukonko/tablewriter/tw"
	"github.com/pterm/pterm"
	"github.com/spf13/pflag"
	"github.com/vale-cli/vale/v3/internal/core"
)

var exampleConfig = `MinAlertLevel = suggestion

	[*]
	BasedOnStyles = Vale`

// intro is the header every help screen opens with.
//
// It is built when it is printed, not when the package loads: it carries
// styling, and whether styling is wanted depends on where the output is going
// -- a question answered after the variables of this package already exist.
func intro() string {
	return fmt.Sprintf(`vale - A command-line linter for prose.

%s:	%s
	%s
	%s

Vale is a syntax-aware linter for prose built with speed and extensibility in
mind. It supports Markdown, AsciiDoc, reStructuredText, HTML, and more.

To get started, you'll need a configuration file (%s):

%s:

	%s

See %s for more setup information.`,
		pterm.Bold.Sprintf("Usage"),

		toCodeStyle("vale [options] [input...]"),
		toCodeStyle("vale myfile.md myfile1.md mydir1"),
		toCodeStyle("vale --output=JSON [input...]"),

		toCodeStyle(".vale.ini"),
		pterm.Bold.Sprintf("Example"),
		toCodeStyle(exampleConfig),

		pterm.Underscore.Sprintf("https://vale.sh"))
}

// info is the intro plus a pointer at the full listing.
func info() string {
	return fmt.Sprintf(`%s

(Or use %s for a listing of all CLI options.)`,
		intro(),
		toCodeStyle("vale --help"))
}

// hidden names the flags the listing leaves out: compatibility shims, the
// switches the test suite and editor integrations set, and flags that work
// but aren't announced yet.
var hidden = []string{
	"built",
	"counts", // Not announced yet: the summary shape is still settling.
	"mode-compat",
	"mode-rev-compat",
	"normalize",
	"relative",
	"sort",
	"sources",
}

// commonFlags lead the listing, ahead of the ones that shape output for a
// script rather than for a person.
var commonFlags = []string{
	"config",
	"output",
	"minAlertLevel",
	"glob",
	"filter",
	"help",
	"version",
}

// PrintIntro shows basic usage / getting started info.
func PrintIntro() {
	fmt.Println(info())
	os.Exit(0)
}

func toFlag(name string) string {
	if code, ok := shortcodes[name]; ok {
		return fmt.Sprintf("%s, %s", toCodeStyle("-"+code), toCodeStyle("--"+name))
	}
	return toCodeStyle("--" + name)
}

// PrintUsage writes the full listing of flags and commands.
//
// The destination is the caller's to choose, and it is the difference between
// the two reasons this gets printed: help that was asked for is output, and
// belongs on stdout, while help that follows a mistake is a diagnostic and
// belongs on stderr. Nor does this exit -- the first case is a success and the
// second is not.
func PrintUsage(w io.Writer) {
	fmt.Fprintln(w, intro())

	// Common first. Alphabetical order put `--ext` and `--filter` ahead of
	// `--output`, which is the flag most runs actually pass.
	var common, rest [][]string
	pflag.VisitAll(func(f *pflag.Flag) {
		if core.StringInSlice(f.Name, hidden) {
			return
		}
		row := []string{toFlag(f.Name), f.Usage}
		if core.StringInSlice(f.Name, commonFlags) {
			common = append(common, row)
		} else {
			rest = append(rest, row)
		}
	})

	slices.SortFunc(common, func(a, b []string) int {
		return slices.Index(commonFlags, flagName(a[0])) -
			slices.Index(commonFlags, flagName(b[0]))
	})

	section(w, "Flags:", common)
	section(w, "More flags:", rest)

	names := make([]string, 0, len(commands))
	for name, cmd := range commands {
		if !cmd.Hidden {
			names = append(names, name)
		}
	}
	slices.Sort(names)

	listing := make([][]string, 0, len(names))
	for _, name := range names {
		listing = append(listing, []string{toCodeStyle(name), commands[name].Summary})
	}
	section(w, "Commands:", listing)

	fmt.Fprintf(w, "Run %s for a command's own help.\n\n",
		toCodeStyle("vale <command> --help"))
}

// PrintCommandUsage writes one command's help.
func PrintCommandUsage(w io.Writer, name string, cmd command) {
	fmt.Fprintf(w, "vale %s - %s\n\n", name, cmd.Summary)

	usage := cmd.Usage
	if usage == "" {
		usage = name
	}
	fmt.Fprintf(w, "%s:\t%s\n", pterm.Bold.Sprintf("Usage"), toCodeStyle("vale "+usage))

	if cmd.Detail != "" {
		fmt.Fprintf(w, "\n%s\n", cmd.Detail)
	}

	fmt.Fprintf(w, "\nSee %s for more.\n", pterm.Underscore.Sprintf("https://vale.sh"))
}

// section renders one titled block of the listing, or nothing when empty.
func section(w io.Writer, title string, rows [][]string) {
	if len(rows) == 0 {
		return
	}

	fmt.Fprintln(w, pterm.Bold.Sprint("\n"+title))
	fmt.Fprintln(w)

	table := newBorderlessTable(w, tw.WrapNone)
	for _, row := range rows {
		table.Append(row)
	}
	table.Render()
	fmt.Fprintln(w)
}

// flagName recovers a flag's name from a rendered cell, which may carry a
// shortcode and styling.
func flagName(cell string) string {
	plain := core.StripANSI(cell)
	if _, long, found := strings.Cut(plain, "--"); found {
		return long
	}
	return plain
}

func init() {
	// ContinueOnError hands a parse error back rather than printing it and
	// exiting. pflag's own handling wrote the message to both streams and took
	// the exit out of main's hands; `vale --bogus` exited 0 for want of the
	// one thing a script reads.
	pflag.CommandLine.Init(os.Args[0], pflag.ContinueOnError)
	pflag.CommandLine.SetOutput(os.Stderr)

	pflag.Usage = func() { PrintUsage(os.Stderr) }
}
