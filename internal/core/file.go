package core

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/jdkato/prose/v3/summarize"
	"github.com/jdkato/prose/v3/tag"

	"github.com/errata-ai/vale/v3/internal/nlp"
	"github.com/errata-ai/vale/v3/internal/system"
)

var commentControlRE = regexp.MustCompile(`^vale (.+\..+|[^.]+) = (YES|NO|on|off)$`)

var commentStyleRE = regexp.MustCompile(`^vale styles? = (.*)$`)

var commentControlMatchesRE = regexp.MustCompile(`^vale (.+\..+)(\[.+\]) = (YES|NO)$`)

// A File represents a linted text file.
type File struct {
	NLP        nlp.Info          // -
	Summary    bytes.Buffer      // holds content to be included in summarization checks
	Alerts     []Alert           // all alerts associated with this file
	BaseStyles []string          // base style assigned in .vale
	Lines      []string          // the File's Content split into lines
	Sequences  []string          // tracks various info (e.g., defined abbreviations)
	Content    string            // the raw file contents
	Format     string            // 'code', 'markup' or 'prose'
	NormedExt  string            // the normalized extension (see util/format.go)
	Path       string            // the full path
	NormedPath string            // the normalized path
	Transform  string            // XLST transform
	RealExt    string            // actual file extension
	Checks     map[string]bool   // syntax-specific checks assigned in .vale
	ChkToCtx   map[string]string // maps a temporary context to a particular check

	// Levels holds the level each rule was given by the sections matching this
	// file, which is not the same as the level it was compiled with: the same
	// rule can be an error in one format and a warning in another. See #965.
	Levels map[string]string

	// chkMasked counts, per check and match text, the occurrences already
	// masked into ChkToCtx this block. An occurrence a prior alert consumed
	// is gone from the context, so it must not widen a later alert's
	// occurrence skip. Reset with ChkToCtx, in StartBlock.
	chkMasked map[string]int

	// sanShifts records, per line, where the sanitizer's `&rsquo;` rewrite
	// shortened the text, so spans can be mapped back to the file's bytes.
	sanShifts  map[int][]int
	Comments   map[string]bool            // comment control statements
	Metrics    map[string]int             // count-based metrics
	history    map[string]int             // -
	limits     map[string]int             // -
	tags       map[string]*nlp.TokenCache // tagging shared by every rule, per model
	lineIdx    []int                      // byte offset of each line start in lineIdxCtx
	lineIdxCtx string                     // the context lineIdx was built from
	simple     bool                       // -
	Lookup     bool                       // -
	MetaScope  string                     // extra scope context, e.g. a YAML key or comment
}

// lineStarts returns the byte offset at which each line of ctx begins.
//
// Locating an alert needs the number of newlines before it. Counting them per
// alert is a scan of the document each time, and alert count grows with
// document size, so the index is built once and searched instead.
func (f *File) lineStarts(ctx string) []int {
	// Comparing the context is cheap when it is the same string, which is the
	// common case: Go checks length and data pointer before any bytes.
	if f.lineIdx != nil && f.lineIdxCtx == ctx {
		return f.lineIdx
	}

	starts := make([]int, 1, strings.Count(ctx, "\n")+1)
	for i := 0; i < len(ctx); i++ {
		if ctx[i] == '\n' {
			starts = append(starts, i+1)
		}
	}

	f.lineIdx, f.lineIdxCtx = starts, ctx

	return starts
}

// NewFile initializes a File.
func NewFile(src string, config *Config) (*File, error) {
	var format, ext string
	var fbytes []byte
	var lookup bool
	path := src

	if system.FileExists(src) {
		fbytes, _ = os.ReadFile(src)
		if config.Flags.InExt != ".txt" {
			ext, format = FormatFromExt(config.Flags.InExt, config.Formats)
		} else {
			ext, format = FormatFromExt(src, config.Formats)
		}
	} else {
		fbytes = []byte(src)
		lookup = true
		// For stdin, allow an explicit path override to drive path-based config.
		if config.Flags.InPath != "" {
			path = config.Flags.InPath
		} else {
			path = "stdin" + config.Flags.InExt
		}
		// If --ext was explicitly set, respect it; otherwise infer from the path.
		if config.Flags.InExt != ".txt" {
			ext, format = FormatFromExt(config.Flags.InExt, config.Formats)
		} else {
			ext, format = FormatFromExt(path, config.Formats)
		}
	}

	filepaths := []string{path}
	normed := system.ReplaceFileExt(path, config.Formats)

	baseStyles := config.GBaseStyles
	checks := make(map[string]bool)
	levels := make(map[string]string)

	for _, fp := range filepaths {
		for _, sec := range config.StyleKeys {
			if pat, found := config.SecToPat[sec]; found && pat.Match(fp) {
				baseStyles = config.SBaseStyles[sec]
			}
		}

		for _, sec := range config.RuleKeys {
			if pat, found := config.SecToPat[sec]; found && pat.Match(fp) {
				for k, v := range config.SChecks[sec] {
					checks[k] = v
				}
				// Sections are visited in the order they were written, so a
				// later one wins -- for this file, and no other. See #965.
				for k, v := range config.SLevels[sec] {
					levels[k] = v
				}
			}
		}
	}

	lang := "en"
	if code, found := config.FormatToLang["*"]; found && code != "" {
		lang = code
	}
	for _, sec := range config.RuleKeys {
		if pat, found := config.SecToPat[sec]; found && pat.Match(path) {
			// Sections are visited in the order they were written, so a
			// later one wins -- for this file, and no other. See #965.
			if code, ok := config.FormatToLang[sec]; ok {
				lang = code
			}
		}
	}

	transform := ""
	if p, found := config.Stylesheets["*"]; found {
		transform = p
	}
	for _, sec := range config.RuleKeys {
		if pat, found := config.SecToPat[sec]; found && pat.Match(path) {
			// Sections are visited in the order they were written, so a
			// later one wins -- for this file, and no other. See #965.
			if p, ok := config.Stylesheets[sec]; ok {
				transform = p
			}
		}
	}
	content := Sanitize(string(fbytes))

	// NOTE: We need to perform a clone here because we perform inplace editing
	// of the files contents that we don't want reflected in `lines`.
	//
	// See lint/walk.go.
	lines := strings.SplitAfter(strings.Clone(content), "\n")

	file := File{
		NormedExt: ext, Format: format, RealExt: filepath.Ext(path),
		BaseStyles: baseStyles, Checks: checks, Levels: levels,
		Lines: lines, Content: content,
		Comments: make(map[string]bool), history: make(map[string]int),
		simple: config.Flags.Simple, Transform: transform,
		limits: make(map[string]int), Path: path, Metrics: make(map[string]int),
		NLP:    nlp.Info{Endpoint: config.NLPEndpoint, Lang: lang},
		Lookup: lookup, NormedPath: normed,
		sanShifts: findShifts(string(fbytes)),
	}

	return &file, nil
}

// Tokens returns the tagged tokens of text, tagging it only once per document
// however many rules ask for it.
func (f *File) Tokens(text string) []tag.Token {
	toks, _ := f.TokensWith("", text)
	return toks
}

// TokensWith is Tokens, read with the named tagger.
//
// Two models disagree about the same sentence -- that is why a rule names
// one -- so each is cached separately.
func (f *File) TokensWith(model, text string) ([]tag.Token, error) {
	if f.tags == nil {
		f.tags = map[string]*nlp.TokenCache{}
	}

	cache, ok := f.tags[model]
	if !ok {
		cache = &nlp.TokenCache{}
		f.tags[model] = cache
	}

	return cache.TokensWith(model, text, &f.NLP)
}

// StartBlock resets the per-block alert state: the masked contexts, and the
// counts of what was masked into them.
func (f *File) StartBlock() {
	f.ChkToCtx = make(map[string]string)
	f.chkMasked = make(map[string]int)
}

// MapAlertsToSource shifts and widens alert spans to account for the
// sanitizer's `&rsquo;` rewrite, so every span indexes the file's actual
// text rather than the sanitized copy that was linted. See #687.
func (f *File) MapAlertsToSource() {
	if len(f.sanShifts) == 0 {
		return
	}

	for i := range f.Alerts {
		a := &f.Alerts[i]
		if len(a.Span) < 2 || a.Span[0] <= 0 {
			continue
		}

		d0, d1 := 0, 0
		for _, col := range f.sanShifts[a.Line] {
			if col < a.Span[0] {
				// A rewrite before the match moves the whole span right.
				d0 += rsquoShift
				d1 += rsquoShift
			} else if col <= a.Span[1] {
				// A rewrite inside the match widens it.
				d1 += rsquoShift
			}
		}

		a.Span[0] += d0
		a.Span[1] += d1
	}
}

// SortedAlerts returns all of f's alerts sorted by line and column.
func (f *File) SortedAlerts() []Alert {
	sort.Sort(ByPosition(f.Alerts))
	return f.Alerts
}

// ComputeMetrics returns all of f's metrics.
func (f *File) ComputeMetrics() (map[string]interface{}, error) {
	params := map[string]interface{}{}

	doc := summarize.NewDocument(f.Summary.String())
	if doc.NumWords == 0 {
		return params, nil
	}

	for k, v := range f.Metrics {
		if strings.HasPrefix(k, "table") {
			continue
		}
		k = strings.ReplaceAll(k, ".", "_")
		params[k] = float64(v)
	}

	params["complex_words"] = doc.NumComplexWords
	params["long_words"] = doc.NumLongWords
	params["sentences"] = doc.NumSentences
	params["characters"] = doc.NumCharacters
	params["words"] = doc.NumWords
	params["polysyllabic_words"] = doc.NumPolysylWords
	params["syllables"] = doc.NumSyllables

	return params, nil
}

// FindLoc calculates the line and span of an Alert.
//
// `at` is where `s` begins in `ctx`, or -1 when that is not known; it lets the
// match be placed without searching for it.
func (f *File) FindLoc(ctx, s string, pad, count int, a Alert, at int) (int, []int) {
	var length int
	var lines []string

	for _, s := range a.Offset {
		ctx, _ = Substitute(ctx, s, '@')
	}

	pos, substring := initialPosition(ctx, s, a, at)
	if pos < 0 {
		// Shouldn't happen ...
		return pos, []int{0, 0}
	}

	loc := a.Span
	if f.Format == "markup" && !f.simple || f.Format == "fragment" {
		lines = f.Lines
	} else {
		lines = strings.SplitAfter(ctx, "\n")
	}

	counter := 0
	for idx, l := range lines {
		length = nlp.StrLen(l)
		if (counter + length) >= pos {
			loc[0] = (pos - counter) + pad
			loc[1] = loc[0] + nlp.StrLen(substring) - 1
			extent := length + pad
			if loc[1] > extent {
				loc[1] = extent
			} else if loc[1] <= 0 {
				loc[1] = 1
			}
			return count - (len(lines) - (idx + 1)), loc
		}
		counter += length
	}

	return count, loc
}

func (f *File) assignLoc(ctx string, blk nlp.Block, pad int, a Alert) (int, []int) {
	loc := a.Span
	for idx, l := range strings.SplitAfter(ctx, "\n") {
		if loc[0] < 0 || loc[1] < 0 {
			continue
		}
		// NOTE: This fixes #473, but the real issue is that `blk.Line` is
		// wrong. This seems related to `location.go#41`, but I'm not sure.
		//
		// At the very least, this change includes a representative test case
		// and a temporary fix.
		exact := len(l) > loc[1] && l[loc[0]:loc[1]] == a.Match
		if exact || idx == blk.Line {
			length := nlp.StrLen(l)
			// Mask the words preceding the match (computed in AddAlert when
			// the token occurs more than once) so the re-search below lands
			// on the right occurrence -- e.g. a line-final `and$` rather than
			// an earlier `and`. FindLoc already does this; assignLoc didn't.
			// See #892.
			masked := l
			for _, s := range a.Offset {
				masked, _ = Substitute(masked, s, '@')
			}
			// No offset hint: `masked` is a single line, so the block's own
			// offset is measured against something else entirely.
			pos, substring := initialPosition(masked, blk.Text, a, -1)

			loc[0] = pos + pad
			loc[1] = pos + nlp.StrLen(substring) - 1

			extent := length + pad
			if loc[1] > extent {
				loc[1] = extent
			} else if loc[1] <= 0 {
				loc[1] = 1
			}

			return idx + 1, loc
		}
	}
	return blk.Line + 1, a.Span
}

// locFromByteOffset computes a 1-based line number and a [col, col+len] span
// from absolute byte offsets into the raw document text. This avoids the
// text-search approach used by FindLoc/initialPosition, which can report the
// wrong location when the matched text appears more than once.
func locFromByteOffset(ctx string, starts []int, begin, end, pad int) (int, []int) {
	if begin > len(ctx) {
		begin = len(ctx)
	}

	// The line is the number of starts at or before begin, which is already
	// 1-based because every document starts a line at offset 0.
	line := sort.Search(len(starts), func(i int) bool { return starts[i] > begin })
	lineStart := starts[line-1]

	col := nlp.StrLen(ctx[lineStart:begin]) + 1 + pad
	matchLen := nlp.StrLen(ctx[begin:end])

	span := []int{col, col + matchLen - 1}
	if span[1] <= 0 {
		span[1] = 1
	}

	return line, span
}

// SetText updates the file's content, lines, and history.
func (f *File) SetText(s string) {
	f.Content = s
	f.Lines = strings.SplitAfter(s, "\n")
	f.history = map[string]int{}
}

// SetNormedExt sets the normalized extension of a File.
func (f *File) SetNormedExt(ext string) {
	f.NormedExt = "." + ext
}

// AddAlert calculates the in-text location of an Alert and adds it to a File.
func (f *File) AddAlert(a Alert, blk nlp.Block, lines, pad int, lookup bool) {
	ctx := blk.Context
	if old, ok := f.ChkToCtx[a.Check]; ok {
		ctx = old
	}

	// When the alert carries byte offsets from a script rule and falls within
	// the document, compute line:column directly from those offsets instead of
	// performing a text search. This fixes incorrect position reporting for
	// script rules with `scope: raw` when the matched text appears more than
	// once.
	//
	// We use blk.Context (the original document) rather than ctx, which may
	// have been modified by ChkToCtx substitutions from earlier alerts.
	switch {
	case a.HasByteOffsets && a.Span[0] >= 0 && a.Span[1] <= len(blk.Context):
		a.Line, a.Span = locFromByteOffset(
			blk.Context, f.lineStarts(blk.Context), a.Span[0], a.Span[1], pad)
	case strings.HasPrefix(blk.Scope, "raw") && a.Match != "" &&
		a.Span[0] >= 0 && a.Span[1] <= len(blk.Context) &&
		blk.Context[a.Span[0]:a.Span[1]] == a.Match:
		// For `scope: raw` the block *is* the whole document, so the span is
		// already an exact byte offset into it -- and the reported line needs
		// no further translation (unlike code comments/fragments). Use it
		// directly rather than re-searching a.Match, which can land on an
		// earlier occurrence; this replaces the capped word-masking heuristic
		// that mislocated `^`-anchored matches once the document exceeded 1k
		// bytes. See #869.
		a.Line, a.Span = locFromByteOffset(
			blk.Context, f.lineStarts(blk.Context), a.Span[0], a.Span[1], pad)
	default:
		// For non-raw scopes the block text differs from the source, so the
		// span isn't a usable byte offset; fall back to a text search. When
		// the match's text also occurs earlier in the block, the search must
		// skip those occurrences -- counted here, where the span and the text
		// share a coordinate system. (This replaces masking the words before
		// the match, which read them from a document-sized context sliced at
		// this block-sized span: junk for any block but the first, and
		// capped, for cost, at contexts under 1,000 bytes.)
		//
		// A check that supplies its own Offset words -- `sequence` names the
		// candidates it rejected -- keeps them; the two must not stack.
		if len(a.Offset) == 0 && a.Match != "" &&
			a.Span[0] >= 0 && a.Span[0] <= len(blk.Text) {
			a.skipOcc = countPrior(blk.Text[:a.Span[0]], a.Match)

			// An occurrence a prior alert already masked out of the context
			// is not there to skip past.
			if a.skipOcc > 0 {
				a.skipOcc -= f.chkMasked[a.Check+"\x00"+a.Match]
				if a.skipOcc < 0 {
					a.skipOcc = 0
				}
			}
		}

		if !lookup {
			a.Line, a.Span = f.assignLoc(ctx, blk, pad, a)
		}
		if (!lookup && a.Span[0] < 0) || lookup {
			a.Line, a.Span = f.FindLoc(ctx, blk.Text, pad, lines, a, blk.Offset)
		}
	}

	if a.Span[0] > 0 {
		// Masking the text we just reported keeps the *next* alert from this
		// check finding the same occurrence again. An alert located by byte
		// offset was never found by searching, so there is nothing to mask --
		// and skipping it avoids copying the whole context per alert, which
		// dominated Vale's allocations.
		if !a.HasByteOffsets {
			f.ChkToCtx[a.Check], _ = Substitute(ctx, a.Match, '#')
			if f.chkMasked != nil {
				f.chkMasked[a.Check+"\x00"+a.Match]++
			}
		}
		if !a.Hide {
			// Ensure that we're not double-reporting an Alert:
			entry := strings.Join([]string{
				strconv.Itoa(a.Line),
				strconv.Itoa(a.Span[0]),
				a.Check}, "-")

			if _, found := f.history[entry]; !found {
				// Check rule-assigned limits for reporting:
				count, occur := f.limits[a.Check]
				if (!occur || a.Limit == 0) || count < a.Limit {
					f.Alerts = append(f.Alerts, a)

					f.history[entry] = 1
					if a.Limit > 0 {
						f.limits[a.Check]++
					}
				}
			}
		}
	}
}

// UpdateComments sets a new status based on comment.
func (f *File) UpdateComments(comment string) {
	if comment == "vale off" { //nolint:gocritic
		f.Comments["off"] = true
	} else if comment == "vale on" {
		f.Comments["off"] = false
	} else if commentControlMatchesRE.MatchString(comment) {
		check := commentControlMatchesRE.FindStringSubmatch(comment)
		if len(check) == 4 {
			var parts []string
			if err := json.Unmarshal([]byte(check[2]), &parts); err == nil {
				for i := range parts {
					f.Comments[check[1]+"["+parts[i]+"]"] = check[3] == "NO"
				}
			}
		}
	} else if commentControlRE.MatchString(comment) {
		check := commentControlRE.FindStringSubmatch(comment)
		if len(check) == 3 {
			f.Comments[check[1]] = (check[2] == "NO" || check[2] == "off")
		}
	} else if commentStyleRE.MatchString(comment) {
		for _, style := range f.BaseStyles {
			f.Comments[style] = true
		}
		check := commentStyleRE.FindStringSubmatch(comment)
		for _, style := range strings.Split(check[1], ", ") {
			f.Comments[style] = false
		}
	}
}

// QueryComments checks if there has been an in-text comment for this check.
func (f *File) QueryComments(check string) bool {
	if f.Comments["off"] {
		return true
	}
	if style, _, ok := strings.Cut(check, "."); ok {
		if status := f.Comments[style]; status {
			return true
		}
	}
	if status := f.Comments[check]; status {
		return true
	}
	return false
}

// ResetComments resets the state of all checks back to active.
func (f *File) ResetComments() {
	for check := range f.Comments {
		if check != "off" {
			f.Comments[check] = false
		}
	}
}

// SetMetaScope sets an optional scope appended to each check's scope, giving
// it extra context. It lives on the file because files are linted
// concurrently.
func (f *File) SetMetaScope(scope string) {
	if scope != "" {
		f.MetaScope = "." + scope
	} else {
		f.MetaScope = ""
	}
}

// Level returns the level `name` should report at in this file, falling back
// to the level it was compiled with.
//
// A rule's own level covers it; a level set for its style covers the rest of
// that style, mirroring how the two are resolved at compile time.
func (f *File) Level(name, compiled string) string {
	if level, ok := f.Levels[name]; ok {
		return level
	} else if level, ok = f.Levels[StyleName(name)]; ok {
		return level
	}
	return compiled
}
