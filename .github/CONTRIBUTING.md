# Contributing to Vale

Interested in contributing to Vale? Great&mdash;we welcome contributions of any kind including documentation improvements, bug reports, feature requests, and pull requests.

## Table of Contents

- [Introduction](#introduction)
- [Setting up a Development Environment](#setting-up-a-development-environment)
- [Testing](#testing)
- [Benchmarking](#benchmarking)
- [Code Contribution Guidelines](#code-contribution-guidelines)
- [AI-Assisted Contributions](#ai-assisted-contributions)
- [License Agreement](#license-agreement)
- [Git Commit Message Guidelines](#git-commit-message-guidelines)
- [Terminology](#terminology)

## Introduction

Vale is a natural language linter that supports plain text, markup (Markdown, reStructuredText, AsciiDoc, HTML, DITA, XML, and more), and source code comments. Unlike many similar projects, Vale's primary focus isn't on providing a collection of rules everyone must follow&mdash;instead, Vale aims to be flexible enough to support many different styles.

Vale is written in Go. The command lives in [`cmd/vale/`](../cmd/vale/) and the implementation is split across [`internal/`](../internal/):

| Package | Responsibility |
|:--|:--|
| `check` | Vale's extension points (`existence`, `substitution`, `occurrence`, etc.), including the built-in `Vale.Terms` and `Vale.Spelling` rules. |
| `core` | Core structures used throughout the application (`File`, `Alert`, `Config`) and configuration handling. |
| `glob` | Glob matching for the file and rule filters in `.vale.ini`. |
| `lint` | The linting itself: when to apply rules and how to handle each markup format. Source-code comments are extracted by the tree-sitter parsers in [`lint/code/`](../internal/lint/code/). |
| `nlp` | POS tagging, word tokenization, and sentence segmentation. |
| `regex` | Pattern compilation and the literal prefilter that skips rules that can't match. |
| `spell` | A pure-Go spell checker built on Hunspell-compatible dictionaries. |
| `system` | Filesystem, path, and process helpers. |

Development happens on the `v3` branch, which is also the base branch for pull requests.

If you're looking to improve Vale's documentation, it lives in a separate repository and is published at [docs.vale.sh](https://docs.vale.sh/).

## Setting up a Development Environment

Prerequisites:

* [Go](https://go.dev/dl/) matching the version in [`go.mod`](../go.mod). Building requires `CGO_ENABLED=1` for the tree-sitter parsers.

The end-to-end suite shells out to external converters, so these also need to be on your `$PATH`:

* [Asciidoctor](https://asciidoctor.org/)
* [rst2html](https://docutils.sourceforge.io/docs/user/tools.html#rst2html-py), installed with [docutils](https://pypi.org/project/docutils/) or [Sphinx](https://www.sphinx-doc.org/)
* [xsltproc](http://xmlsoft.org/xslt/xsltproc.html)
* [dita](https://www.dita-ot.org/download) (v3.6+)
* [typst2vast](https://github.com/jdkato/typst2vast) (`cargo install --locked typst2vast`)

Then build and test:

```bash
make build os=linux exe=vale  # writes ./bin/vale
make test                     # go test ./...
```

`make build` takes `os`, `arch`, and `exe`; omitting `os` and `arch` builds for the host platform. On Windows, use `exe=vale.exe`.

## Testing

Vale is tested with both unit and end-to-end tests, and `make test` runs both. `go test ./...` runs them too, but hides the end-to-end progress display&mdash;`go test` throws away a passing package's output, so `make test` builds that suite and runs it directly.

Unit tests are the `*_test.go` files inside the Go packages.

End-to-end tests build `vale`, invoke it the way a user would, and compare its combined output against what the case declares. The runner is [`internal/e2e`](../internal/e2e); the cases are in [`testdata/e2e/`](../testdata/e2e/), one YAML file per suite:

```yaml
# Every markup and source format Vale can lint.
name: lint
dir: fixtures/formats

cases:
  - name: css
    args: test.css
    exit: 1
    want: |
      test.css:1:4:vale.Annotations:'TODO' left in text
      test.css:7:19:vale.Annotations:'XXX' left in text
```

Every case is invoked with `--output=line --sort --normalize --relative --no-global` so that its output is stable across platforms; `args` is appended to those.

| Key | |
| --- | --- |
| `name` | unique within the suite; may contain `/` to group related cases |
| `about` | a note for the reader&mdash;an issue number, or what the case pins down |
| `dir` | working directory, relative to the suite's. A suite `dir` ending in `/*` gives each case a directory named after it |
| `args` | one string, split on spaces honoring quotes; or a list to pass words through verbatim |
| `files` | the case's own files, written to a scratch directory it runs in&mdash;instead of a `dir` |
| `requires` | external converters the case needs; it's skipped where they aren't installed |
| `stdin` | a file in the working directory to pipe in |
| `sync` | run `vale sync` first |
| `exit` | expected exit status |
| `want` | the exact output expected (`want: ""` asserts none) |
| `contains` | an excerpt of it, when the rest is noise |
| `absent` | strings the output must not contain |

A case either runs in a checked-in fixture under `testdata/fixtures/` or brings its own files. A suite can declare `files:` that every case starts from, and each case adds or overrides on top&mdash;so a self-contained project reads in one place:

```yaml
name: config

# Every case starts from these two documents, then adds the configuration
# it needs. Each one runs in a scratch directory built from just these.
files:
  test.md: |
    This is a very important sentence. There is a sentence here too.

cases:
  - name: level-warning
    files:
      .vale: |
        StylesPath = ../../styles/
        MinAlertLevel = warning

        [*]
        BasedOnStyles = vale
    args: test.md
    exit: 0
    want: |
      test.md:1:11:vale.Editorializing:Consider removing 'very'
```

Those scratch directories are built under `testdata/tmp/`, two levels down, so a relative `StylesPath` resolves exactly as it would in a real project.

```bash
# every case, or one suite, or one case
go test ./internal/e2e
go test ./internal/e2e -run 'TestScenarios/scopes'
go test ./internal/e2e -run 'TestScenarios/lint/css'

# the same, with the progress display
make test run='-test.run TestScenarios/scopes'
```

The suite runs on Linux, macOS, and Windows. You don't need the whole documentation toolchain to work on Vale&mdash;a case whose `requires` aren't installed is skipped, and the run tells you what was missing:

```
....s.s..........ss....s...s.........s.....s.......s........

149 cases in 17 suites — 104 passed, 45 skipped (4.7s)
not installed: asciidoctor, dita, rst2html, typst2vast — see .github/CONTRIBUTING.md
```

CI sets `VALE_E2E_STRICT=1`, which turns those skips into failures, so a converter that didn't install can't quietly pass for coverage.

After an intentional change to Vale's output, rewrite the `want:` blocks in place rather than editing them by hand, then review the diff:

```bash
go test ./internal/e2e -update
```

To add a case, append it to the right suite with an empty `want: ""`, run `-update` to fill it in, and check that what it wrote is what you meant.

Both suites run on Linux and Windows in [`test.yml`](workflows/test.yml). The end-to-end suite is Linux-only in CI because its scenarios need the documentation toolchain listed above; Windows covers the build and the unit tests.

## Benchmarking

Benchmarks live alongside the code in `internal/core`, `internal/lint`, and `internal/check`:

```bash
make bench                    # go test -bench=. -benchmem on those packages
make profile                  # CPU, memory, and trace profiles into bin/
```

Every pull request is benchmarked against its merge base by [`bench.yml`](workflows/bench.yml), which reports a `benchstat` comparison plus peak RSS:

```text
                     │  /tmp/old.txt  │             /tmp/new.txt             │
                     │     sec/op     │    sec/op     vs base                │
LintRST-4               1.63 ± 2%       1.65 ± 2%    ~ (p=0.310 n=6)
LintMD-4                1.54 ± 1%       1.42 ± 1%  -7.79% (p=0.002 n=6)
```

Absolute timings from a shared CI runner aren't meaningful, but the ratio between two runs minutes apart on the same box is. If you're submitting a `perf` change, include the comparison in the pull request.

## Code Contribution Guidelines

To make the contribution process as seamless as possible, we ask for the following:

* Fork the project and make your changes against `v3`.
* Add or update tests for the behavior you're changing&mdash;an [`internal/e2e`](../internal/e2e) scenario for user-visible behavior, a unit test for anything internal.
* When you're ready to create a pull request, be sure to:
    * Run [golangci-lint](https://golangci-lint.run/) to check your Go code. The configuration is in [`.golangci.yml`](../.golangci.yml) and CI runs v2.5.
    * Run `gofmt` (or `go fmt ./...`) on anything you've touched.
    * Squash your commits into a single commit with `git rebase -i`. It's okay to force update your pull request with `git push -f`.
    * Follow the **Git Commit Message Guidelines** below.

## AI-Assisted Contributions

You may use whatever tools you like to write a contribution, with these conditions:

* **You are the author.** You must understand every line you submit and be able to answer questions about it in review without going back to the tool. If you can't, don't open the pull request.
* **Say so.** If a substantial part of the code or text was tool-generated, note the tool in the pull request description or a commit trailer such as `Assisted-by: Claude Code`. This helps review; it isn't a mark against the change.
* **Write the description yourself.** State the problem and the fix in a few sentences. A long, sectioned write-up for a small change costs more review time than the change.
* **Don't sweep the tree.** A pull request should fix a bug you hit or build a feature that was agreed in an issue first. Standalone cleanups, dead-code removal, and refactors found by running a tool over the codebase will be closed; fold them into the change that needs them.
* **Undocumented options and hidden commands are out of scope.** If it isn't in the [documentation](https://docs.vale.sh/), it's an experiment and may be removed. Don't add tests or fixes that harden it.
* **No unattended agents.** Tools must not open issues, comment on pull requests, or push commits without a person reading and approving each action.
* **One thing at a time.** This repository uses GitHub's [pull request limits](https://github.blog/open-source/maintainers/how-pull-request-limits-are-cutting-down-the-noise/), which cap how many pull requests a contributor can have open at once. Finish one before opening the next.

## License Agreement

The first time you open a pull request, a bot will ask you to agree to Vale's [contributor license agreement](CLA.md). It's a few sentences, you agree by replying to a comment, and it's once per person, not per pull request.

What it says: you wrote the change or have the right to contribute it, and you let the project use it under its current license and any other license it adopts later. You keep the copyright to your work.

Why it exists: Vale has been MIT-licensed since 2016 and there are no plans to change that. The agreement is insurance, so that if the license ever does need to change, the project won't have to track down every past contributor for permission.

## Git Commit Message Guidelines

Vale follows a modified version of the [AngularJS Commit Guidelines](https://github.com/angular/angular.js/blob/master/CONTRIBUTING.md#-git-commit-guidelines). A commit message should take the following form:

```text
<type>: <subject>
<BLANK LINE>
<body>
<BLANK LINE>
<footer>
```

with `<body>` and `<footer>` being optional. `<type>` should be one of the following:

- `feat`: A new feature
- `fix`: A bug fix
- `docs`: Documentation only changes (e.g., this document, the README, or source comments)
- `style`: Changes that do not affect the meaning of the code (e.g., code formatting)
- `refactor`: A code change that neither fixes a bug nor adds a feature
- `perf`: A code change that improves performance (in this case, please include relevant benchmark(s))
- `test`: Adding missing or correcting existing tests
- `chore`: Changes to the build process or auxiliary tools

An example would be something like:

```text
refactor: make "warning" the default lint level

Also demotes `Annotations` and `PassiveVoice` to "suggestions."

Related to #30.
```

## Terminology

| Term  | Definition |
|:--|:--|
| check | A "check" is one of Vale's extension points (e.g., `existence` and `substitution`) that performs a single task such as looking for the existence of a word. The implementations live in [`internal/check/`](../internal/check/). |
| rule  | A "rule" is an actual implementation of a check, written as a YAML file. For example, `Hedging` in the `write-good` package is an `existence` rule. Browse them in the [Rule Explorer](https://vale.sh/explorer/). |
| style | A "style" is a collection of rules, distributed as a package. For example, `Joblint` is a style that consists of rules such as `LegacyTech`. Browse them in the [Package Hub](https://vale.sh/hub/). |
