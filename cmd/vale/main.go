package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/pflag"

	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/lint"
	"github.com/vale-cli/vale/v3/internal/system"
)

// version is set during the release build process.
var version = "master"

func stat() bool {
	stat, err := os.Stdin.Stat()
	if err != nil || (stat.Mode()&os.ModeCharDevice) != 0 {
		return false
	}
	return true
}

func looksLikeStdin(s string) int {
	isDir := system.IsDir(s)
	if !(system.FileExists(s) || isDir) && s != "" {
		return 1
	} else if isDir {
		return 0
	}
	return -1
}

func doLint(args []string, l *lint.Linter, glob string) ([]*core.File, error) {
	var linted []*core.File
	var err error

	length := len(args)
	if length == 1 && looksLikeStdin(args[0]) == 1 { //nolint:gocritic
		// Case 1:
		//
		// $ vale "some text in a string"
		linted, err = l.LintString(args[0])
	} else if length > 0 {
		// Case 2:
		//
		// $ vale file1 dir1 file2
		input := []string{}
		for _, file := range args {
			status := looksLikeStdin(file)
			if status == 1 {
				return linted, core.NewE100(
					"doLint",
					fmt.Errorf("argument '%s' does not exist", file),
				)
			}
			input = append(input, file)
		}
		linted, err = l.Lint(input, glob)
	} else {
		// Case 3:
		//
		// $ cat file.md | vale
		stdin, readErr := io.ReadAll(os.Stdin)
		if readErr != nil {
			return linted, core.NewE100("doLint", readErr)
		}
		linted, err = l.LintString(string(stdin))
		if err != nil {
			return linted, core.NewE100("doLint", err)
		}
	}

	return linted, err
}

func handleError(err error) {
	ShowError(err, Flags.Output, os.Stderr)
	os.Exit(2)
}

func main() {
	// Every exit path below calls this explicitly: os.Exit skips deferred
	// functions, so a defer here would silently drop the profile.
	stopProfiling := startProfiling()

	if err := pflag.CommandLine.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		PrintUsage(os.Stderr)
		stopProfiling()
		os.Exit(2)
	}
	configureColor()

	args := pflag.Args()
	argc := len(args)

	if Flags.Version {
		fmt.Println("vale version " + version)
		stopProfiling()
		os.Exit(0)
	} else if argc == 0 && !Flags.Help && !stat() {
		PrintIntro()
	}

	// `vale help`, `vale help <command>`.
	if argc > 0 && args[0] == "help" {
		if argc > 1 {
			if cmd, ok := commands[args[1]]; ok && !cmd.Hidden {
				PrintCommandUsage(os.Stdout, args[1], cmd)
				stopProfiling()
				os.Exit(0)
			}
		}
		PrintUsage(os.Stdout)
		stopProfiling()
		os.Exit(0)
	}

	if argc > 0 {
		cmd, exists := commands[args[0]]

		// A bare word that is nearly a command is a typo, not a document.
		// Linting it as one reported "0 errors ... in stdin" and exited 0,
		// which reads as a clean run of something that never ran.
		if !exists && argc == 1 {
			if suggestion, ok := didYouMean(args[0]); ok {
				reportUnknownCommand(args[0], suggestion)
				stopProfiling()
				os.Exit(2)
			}
		}

		if exists {
			// `vale sync --help` is a question about sync, not about Vale.
			if Flags.Help {
				PrintCommandUsage(os.Stdout, args[0], cmd)
				stopProfiling()
				os.Exit(0)
			}

			err := cmd.Run(args[1:], &Flags)
			stopProfiling()

			// Failing test cases mean Vale worked and the configuration did
			// not, which is the same distinction `vale file.md` draws between
			// exiting 1 and exiting 2. The command has already reported them.
			if errors.Is(err, errTestFailed) {
				os.Exit(1)
			} else if err != nil {
				handleError(err)
			}

			os.Exit(0)
		}
	}

	// Help asked for without a command is a question about Vale.
	if Flags.Help {
		PrintUsage(os.Stdout)
		stopProfiling()
		os.Exit(0)
	}

	config, err := core.ReadPipeline(&Flags, false)
	if err != nil {
		handleError(err)
	}

	linter, err := lint.NewLinter(config)
	if err != nil {
		handleError(err)
	}

	linted, err := doLint(args, linter, Flags.Glob)
	if err != nil {
		handleError(err)
	}

	if config.Flags.Counts && config.Flags.Output == "JSON" {
		hasErrors := PrintJSONAlertsWithCounts(linted, countAlerts(linter.Manager, linted))
		stopProfiling()
		if hasErrors && !Flags.NoExit {
			os.Exit(1)
		}
		os.Exit(0)
	}

	hasErrors, err := PrintAlerts(linted, config)
	if err != nil {
		handleError(err)
	}

	stopProfiling()
	if hasErrors && !Flags.NoExit {
		os.Exit(1)
	}
	os.Exit(0)
}
