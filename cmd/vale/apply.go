package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/vale-cli/vale/v3/internal/check"
	"github.com/vale-cli/vale/v3/internal/core"
	"github.com/vale-cli/vale/v3/internal/lint"
	"github.com/vale-cli/vale/v3/internal/system"
)

// A fixEdit is one unambiguous fix, resolved to the byte span it rewrites.
type fixEdit struct {
	begin, end int
	text       string
	check      string
	line, col  int
}

// A fixSkip is an alert `--apply` chose not to touch, and why.
type fixSkip struct {
	Check  string `json:"Check"`
	Line   int    `json:"Line"`
	Span   []int  `json:"Span"`
	Reason string `json:"Reason"`
}

// A fixedFile is one file's report: what was written and what was left.
type fixedFile struct {
	Path    string    `json:"Path"`
	Applied int       `json:"Applied"`
	Skipped []fixSkip `json:"Skipped"`
}

// applyFixes lints the given paths and writes every unambiguous fix back to
// disk: an alert whose action resolves to exactly one suggestion, at a span
// no other fix touches. Everything else is reported, not guessed at -- an
// alert with several suggestions, one whose fix overlaps another's, or one
// whose reported position no longer holds its match. A file with nothing to
// apply is never rewritten.
func applyFixes(paths []string, flags *core.CLIFlags) error {
	if len(paths) == 0 {
		return core.NewE100("fix", errors.New("at least one path expected"))
	}
	for _, p := range paths {
		if !system.FileExists(p) && !system.IsDir(p) {
			return core.NewE100("fix", fmt.Errorf("path '%s' does not exist", p))
		}
	}

	cfg, err := core.ReadPipeline(flags, false)
	if err != nil {
		return err
	}

	linter, err := lint.NewLinter(cfg)
	if err != nil {
		return err
	}

	linted, err := linter.Lint(paths, flags.Glob)
	if err != nil {
		return err
	}

	var reports []fixedFile
	for _, f := range linted {
		report, fErr := fixFile(f, cfg)
		if fErr != nil {
			return fErr
		}
		if report.Applied > 0 || len(report.Skipped) > 0 {
			reports = append(reports, report)
		}
	}

	if flags.Output == "JSON" {
		return printJSON(reports)
	}

	for _, r := range reports {
		fmt.Printf("%s: applied %d, skipped %d\n", r.Path, r.Applied, len(r.Skipped))
		for _, s := range r.Skipped {
			fmt.Printf("  %d:%d\t%s\t%s\n", s.Line, s.Span[0], s.Check, s.Reason)
		}
	}

	return nil
}

// fixFile resolves and applies one file's fixes, reporting what it did.
func fixFile(f *core.File, cfg *core.Config) (fixedFile, error) {
	report := fixedFile{Path: f.Path, Skipped: []fixSkip{}}

	raw, err := os.ReadFile(f.Path)
	if err != nil {
		return report, err
	}
	starts := lineOffsets(raw)

	skip := func(a core.Alert, reason string) {
		report.Skipped = append(report.Skipped, fixSkip{
			Check: a.Check, Line: a.Line, Span: a.Span, Reason: reason})
	}

	// Repeated identical misspellings resolve to the same suggestions, and
	// the spelling fixer builds a Manager per call; one lookup each is
	// plenty.
	type resolved struct {
		suggestions []string
		err         string
	}
	cache := map[string]resolved{}

	var candidates []fixEdit
	for _, a := range f.SortedAlerts() {
		if a.Action.Name == "" {
			continue
		}

		key := a.Check + "\x00" + a.Match
		got, ok := cache[key]
		if !ok {
			suggestions, fErr := check.FixAlert(a, cfg)
			if fErr != nil {
				got.err = fErr.Error()
			}
			got.suggestions = suggestions
			cache[key] = got
		}

		if got.err != "" {
			skip(a, got.err)
			continue
		}
		if n := len(got.suggestions); n != 1 {
			skip(a, fmt.Sprintf("%d suggestions", n))
			continue
		}

		begin, end, ok := byteSpan(raw, starts, a)
		if !ok {
			skip(a, "match is not at its reported position")
			continue
		}

		candidates = append(candidates, fixEdit{
			begin: begin, end: end, text: got.suggestions[0],
			check: a.Check, line: a.Line, col: a.Span[0]})
	}

	kept := resolveEdits(candidates, &report)
	if len(kept) == 0 {
		report.Applied = 0
		return report, nil
	}

	out := make([]byte, 0, len(raw))
	last := 0
	for _, e := range kept {
		out = append(out, raw[last:e.begin]...)
		out = append(out, e.text...)
		last = e.end
	}
	out = append(out, raw[last:]...)

	mode := fs.FileMode(0o600)
	if info, sErr := os.Stat(f.Path); sErr == nil {
		mode = info.Mode()
	}
	if err = os.WriteFile(f.Path, out, mode); err != nil {
		return report, err
	}

	report.Applied = len(kept)
	return report, nil
}

// resolveEdits orders the candidates and keeps the ones that stand alone.
// Two rules asking for the same rewrite collapse into one; a span touched by
// two different rewrites is a conflict, and every member of an overlapping
// cluster is reported rather than resolved.
func resolveEdits(candidates []fixEdit, report *fixedFile) []fixEdit {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].begin != candidates[j].begin {
			return candidates[i].begin < candidates[j].begin
		}
		return candidates[i].end < candidates[j].end
	})

	deduped := candidates[:0]
	for _, c := range candidates {
		n := len(deduped)
		if n > 0 && deduped[n-1].begin == c.begin &&
			deduped[n-1].end == c.end && deduped[n-1].text == c.text {
			continue
		}
		deduped = append(deduped, c)
	}

	var kept []fixEdit
	for i := 0; i < len(deduped); {
		j, maxEnd := i+1, deduped[i].end
		for j < len(deduped) && deduped[j].begin < maxEnd {
			maxEnd = max(maxEnd, deduped[j].end)
			j++
		}

		if j == i+1 {
			kept = append(kept, deduped[i])
		} else {
			names := make([]string, 0, j-i)
			for k := i; k < j; k++ {
				names = append(names, deduped[k].check)
			}
			for k := i; k < j; k++ {
				report.Skipped = append(report.Skipped, fixSkip{
					Check: deduped[k].check,
					Line:  deduped[k].line,
					Span:  []int{deduped[k].col},
					Reason: "overlaps " + strings.Join(
						append(names[:k-i:k-i], names[k-i+1:]...), ", "),
				})
			}
		}
		i = j
	}

	return kept
}

// byteSpan maps an alert's line and character columns onto the bytes of the
// file as it sits on disk, refusing the mapping unless those bytes are the
// alert's own match -- the file may have changed since the lint pass, or the
// span may describe text extraction rewrote.
func byteSpan(raw []byte, starts []int, a core.Alert) (int, int, bool) {
	if len(a.Span) != 2 || a.Span[0] < 1 || a.Span[1] < a.Span[0] ||
		a.Line < 1 || a.Line > len(starts) {
		return 0, 0, false
	}

	ls := starts[a.Line-1]
	le := len(raw)
	if a.Line < len(starts) {
		le = starts[a.Line]
	}
	line := string(raw[ls:le])

	begin := runeIndex(line, a.Span[0]-1)
	end := runeIndex(line, a.Span[1])
	if begin < 0 || end < 0 {
		return 0, 0, false
	}

	b, e := ls+begin, ls+end
	if string(raw[b:e]) != a.Match {
		return 0, 0, false
	}
	return b, e, true
}

// runeIndex returns the byte index of the n-th rune of s, or -1 when s ends
// first.
func runeIndex(s string, n int) int {
	i := 0
	for ; n > 0; n-- {
		if i >= len(s) {
			return -1
		}
		_, w := utf8.DecodeRuneInString(s[i:])
		i += w
	}
	return i
}

// lineOffsets returns the byte offset at which each line of raw begins.
func lineOffsets(raw []byte) []int {
	starts := []int{0}
	for i, b := range raw {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}
