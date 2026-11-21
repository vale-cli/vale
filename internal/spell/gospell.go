package spell

import (
	"bufio"
	"bytes"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/adrg/strutil"
	"github.com/adrg/strutil/metrics"

	"github.com/vale-cli/vale/v3/internal/system"
)

type wordMatch struct {
	word  string
	score float64

	listed bool // from an ignore list rather than the dictionary
}

// goSpell checks words against one dictionary. The dictionary is shared by
// every checker that loads the same files; the ignore-list words and the
// lookup caches are a checker's own.
type goSpell struct {
	*dictData

	// listed holds the words an ignore list added. They match as written
	// or lower-cased, whatever the input's case, as they always have.
	listed map[string]struct{}

	// Lookups are memoized, since a document repeats its words and a
	// checker is shared by the files linted in parallel.
	mu       sync.Mutex
	spelled  map[string]bool
	segments map[string]segmentUse
}

// dictData is what a `.aff` and `.dic` pair loads to. It is never written
// after loading, so checkers share it.
type dictData struct {
	affix *dictConfig

	// roots holds the `.dic` entries by word, one per homonym line. Their
	// forms are generated on lookup rather than at load, which is what
	// keeps an inflected language's dictionary in memory.
	roots map[string][]rootEntry
	// upperRoots maps the upper-cased form of each root with a capital to
	// the roots it stands for, Hunspell's hidden upper-case homonym.
	upperRoots map[string][]string
	// forbiddenRoots holds the FORBIDDENWORD entries; their forms are not
	// words, and no other reading of one may accept it.
	forbiddenRoots map[string][]rootEntry
	// pairs holds the run-together form of each two-word entry, which is
	// not a compound.
	pairs map[string]struct{}

	ireplacer   *strings.Replacer
	compounds   []*regexp.Regexp
	splitter    *splitter
	canCompound bool // dictionary uses COMPOUNDFLAG/BEGIN/MIDDLE/END
	compoundMin int
	checkDup    bool // CHECKCOMPOUNDDUP
	checkTriple bool // CHECKCOMPOUNDTRIPLE
	simplified  bool // SIMPLIFIEDTRIPLE
	checkCase   bool // CHECKCOMPOUNDCASE
	checkRep    bool // CHECKCOMPOUNDREP
	wordMax     int  // COMPOUNDWORDMAX, or 0
	syllableMax int  // COMPOUNDSYLLABLE, or 0
	vowels      string
	reps        [][2]string
	patterns    []compoundPattern
	breaks      []breakRule
	ignored     []string // .aff directives the reader does not implement
}

// fork returns a checker over the same dictionary with its own ignore list
// and caches.
func (s *goSpell) fork() *goSpell {
	return &goSpell{
		dictData: s.dictData,
		listed:   make(map[string]struct{}),
		spelled:  make(map[string]bool),
		segments: make(map[string]segmentUse),
	}
}

// rootEntry is one `.dic` line's flags.
type rootEntry struct {
	flags []string
}

// segmentUse is a cached segment lookup.
type segmentUse struct {
	seg segment
	ok  bool
}

// breakRule is one Hunspell BREAK pattern: a literal that a word may be
// split on, or, when anchored, stripped from one end.
type breakRule struct {
	pattern string
	atStart bool // written `^pattern`
	atEnd   bool // written `pattern$`
}

// maxBreakDepth bounds how many times a word may be split by BREAK rules.
const maxBreakDepth = 10

// defaultBreaks is what Hunspell uses when a dictionary declares no BREAK.
var defaultBreaks = []string{"-", "^-", "-$"}

func newBreakRules(patterns []string) []breakRule {
	rules := make([]breakRule, 0, len(patterns))
	for _, p := range patterns {
		r := breakRule{pattern: p}
		if len(r.pattern) > 1 && strings.HasPrefix(r.pattern, "^") {
			r.atStart = true
			r.pattern = r.pattern[1:]
		}
		if len(r.pattern) > 1 && strings.HasSuffix(r.pattern, "$") {
			r.atEnd = true
			r.pattern = r.pattern[:len(r.pattern)-1]
		}
		if r.pattern != "" {
			rules = append(rules, r)
		}
	}
	return rules
}

type dictionary struct {
	dic string
	aff string
}

// inputConversion does any character substitution before checking
//
//	This is based on the ICONV stanza
func (s *goSpell) inputConversion(raw []byte) string {
	sraw := string(raw)
	if s.ireplacer == nil {
		return sraw
	}
	return s.ireplacer.Replace(sraw)
}

// addWordRaw adds a single word to the internal dictionary without modifications
// returns true if added
// return false is already exists
func (s *goSpell) addWordRaw(word string) bool {
	if _, ok := s.listed[word]; ok {
		// already exists
		return false
	}
	s.listed[word] = struct{}{}
	s.mu.Lock()
	s.spelled = make(map[string]bool)
	s.mu.Unlock()
	return true
}

// addWordListFile reads in a word list file
func (s *goSpell) addWordListFile(fsys system.FS, name string) ([]string, error) {
	fd, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer fd.Close()
	return s.addWordList(fd)
}

// addWordList adds basic word lists, just one word per line
//
//	Assumed to be in UTF-8
//
// TODO: hunspell compatible with "*" prefix for forbidden words
// and affix support
// returns list of duplicated words and/or error
func (s *goSpell) addWordList(r io.Reader) ([]string, error) {
	var duplicates []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		word := strings.TrimSpace(scanner.Text())
		if len(word) == 0 || word == "#" {
			continue
		}
		if !s.addWordRaw(word) {
			duplicates = append(duplicates, word)
		}
	}
	if err := scanner.Err(); err != nil {
		return duplicates, err
	}
	return duplicates, nil
}

func (s *goSpell) keys() []string {
	keys := make([]string, 0, len(s.roots))
	for k := range s.roots {
		keys = append(keys, k)
	}
	return keys
}

func (s *goSpell) suggest(word string) []wordMatch {
	var hits []wordMatch
	seen := map[string]struct{}{word: {}}
	add := func(w string) {
		if _, ok := seen[w]; ok {
			return
		}
		seen[w] = struct{}{}
		// Earlier is better; the score keeps that order across checkers.
		hits = append(hits, wordMatch{word: w, score: 1 - float64(len(hits))/1e6})
	}

	// The word in another case, then the dictionary's own spelling of it,
	// then what the REP table and a split make of it: Hunspell's order.
	lower := s.lowerWord(word)
	for _, v := range []string{lower, capitalize(lower), s.upperKey(word)} {
		if v != word && s.spellDepth(v, 0) {
			add(v)
		}
	}
	for _, f := range append(s.analyses(s.upperKey(word), true), s.analyses(lower, false)...) {
		if strings.EqualFold(f.Word, word) && !hasFlag(f.Flags, s.affix.CompoundOnly) {
			add(f.Word)
		}
	}
	for _, rep := range s.reps {
		from, to := rep[0], strings.ReplaceAll(rep[1], "_", " ")
		for at := strings.Index(word, from); at >= 0; {
			candidate := word[:at] + to + word[at+len(from):]
			if s.spellPhrase(candidate) {
				add(candidate)
			}
			next := strings.Index(word[at+1:], from)
			if next < 0 {
				break
			}
			at += 1 + next
		}
	}
	for i := 1; i < len(word); i++ {
		if left, right := word[:i], word[i:]; s.spellDepth(left, 0) && s.spellDepth(right, 0) {
			add(left + " " + right)
		}
	}
	if len(hits) >= 5 {
		return hits[:5]
	}

	// Then the closest words. Roots are ranked first, and the forms of the
	// closest ones after, so an inflected form is still found without
	// generating every form. Distance is measured case-insensitively; the
	// case comes back below.
	metric := metrics.NewLevenshtein()
	roots := []wordMatch{}
	for _, option := range s.keys() {
		sim := strutil.Similarity(option, lower, metric)
		roots = append(roots, wordMatch{word: option, score: sim})
	}

	// A word from an ignore list is the project's own, and comes first
	// among candidates it ties with.
	listed := []wordMatch{}
	for option := range s.listed {
		sim := strutil.Similarity(s.lowerWord(option), lower, metric)
		listed = append(listed, wordMatch{word: option, score: sim, listed: true})
	}
	sort.Slice(roots, func(i, j int) bool {
		if roots[i].score != roots[j].score {
			return roots[i].score > roots[j].score
		}
		return roots[i].word < roots[j].word // a tie is broken the same way every run
	})
	if len(roots) > suggestRoots {
		roots = roots[:suggestRoots]
	}

	formSeen := map[string]struct{}{}
	matches := []wordMatch{}
	e := s.affix.expander(nil, nil)
	for _, r := range roots {
		for _, entry := range s.roots[r.word] {
			for _, f := range e.root(r.word, entry.flags) {
				if _, ok := formSeen[f.Word]; ok || len(formSeen) > suggestForms {
					continue
				}
				formSeen[f.Word] = struct{}{}
				sim := strutil.Similarity(f.Word, lower, metric)
				matches = append(matches, wordMatch{word: f.Word, score: sim})
			}
		}
	}
	matches = append(matches, listed...)
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score != matches[j].score {
			return matches[i].score > matches[j].score
		}
		if matches[i].listed != matches[j].listed {
			return matches[i].listed
		}
		return matches[i].word < matches[j].word
	})

	// Suggestions take the case the word was written in.
	restore := func(w string) string { return w }
	switch {
	case allUpper(word):
		restore = s.upperKey
	case initialUpper(word):
		restore = capitalize
	}
	for _, m := range matches {
		if len(hits) >= 5 {
			break
		}
		w := restore(m.word)
		if _, ok := seen[w]; ok {
			continue
		}
		seen[w] = struct{}{}
		hits = append(hits, wordMatch{word: w, score: m.score, listed: m.listed})
	}
	return hits
}

// spellPhrase reports whether every space-separated part of a phrase is a
// word, for a REP entry that writes one word as two.
func (s *goSpell) spellPhrase(phrase string) bool {
	for _, part := range strings.Fields(phrase) {
		if !s.spellDepth(part, 0) {
			return false
		}
	}
	return true
}

// suggestRoots is how many of the closest roots are expanded for
// suggestions, and suggestForms caps the forms considered.
const (
	suggestRoots = 20
	suggestForms = 20000
)

// allUpper reports whether word has letters and every one is upper-case.
func allUpper(word string) bool {
	letters := false
	for _, r := range word {
		if !unicode.IsLetter(r) {
			continue
		}
		if !unicode.IsUpper(r) {
			return false
		}
		letters = true
	}
	return letters
}

// initialUpper reports whether word starts with an upper-case letter.
func initialUpper(word string) bool {
	for _, r := range word {
		return unicode.IsUpper(r)
	}
	return false
}

// spell checks to see if a given word is in the internal dictionaries
func (s *goSpell) spell(word string) bool {
	s.mu.Lock()
	ok, known := s.spelled[word]
	s.mu.Unlock()
	if known {
		return ok
	}

	// The input is read as the dictionary is: through ICONV, and without
	// the IGNORE characters.
	ok = s.spellDepth(s.affix.ignore(s.inputConversion([]byte(word))), 0)

	s.mu.Lock()
	if len(s.spelled) > cacheMax {
		s.spelled = make(map[string]bool)
	}
	s.spelled[word] = ok
	s.mu.Unlock()
	return ok
}

// cacheMax bounds the lookup caches.
const cacheMax = 1 << 16

// isForbidden reports whether a FORBIDDENWORD entry generates word. Hunspell
// reads homonyms in order and the first decides, so a word that is itself a
// root of another entry stays valid.
func (s *goSpell) isForbidden(word string) bool {
	if len(s.forbiddenRoots) == 0 {
		return false
	}
	if _, ok := s.roots[word]; ok {
		return false
	}
	return len(s.readings(word, false, s.forbiddenRoots)) > 0
}

// isWord reports whether word is a form some entry generates and may stand
// on its own. A folded word, one whose case was changed to look it up, is
// not a KEEPCASE form.
func (s *goSpell) isWord(word string, folded bool) bool {
	if s.isForbidden(word) {
		return false
	}
	for _, f := range s.analyses(word, false) {
		if hasFlag(f.Flags, s.affix.CompoundOnly) {
			continue
		}
		// Under CHECKSHARPS, KEEPCASE on a word with ß only says its
		// all-caps form is written with SS; it may still be capitalized.
		if folded && hasFlag(f.Flags, s.affix.KeepCaseFlag) && !s.hasSharp(f.Word) {
			continue
		}
		return true
	}
	return false
}

// isWordUpper reports whether an all-caps word is the upper-cased form of
// one some entry generates. Under CHECKSHARPS, a KEEPCASE word with ß may be
// upper-cased, but only written with SS.
func (s *goSpell) isWordUpper(word string) bool {
	for _, f := range s.analyses(s.upperKey(word), true) {
		if s.isForbidden(f.Word) {
			continue
		}
		if hasFlag(f.Flags, s.affix.CompoundOnly) {
			continue
		}
		if hasFlag(f.Flags, s.affix.KeepCaseFlag) && !(s.hasSharp(f.Word) && !s.hasSharp(word)) {
			continue
		}
		return true
	}
	return false
}

// caseType is Hunspell's classification of a word by its capitals.
type caseType int

const (
	noCap      caseType = iota // hello
	initCap                    // Hello
	allCap                     // HELLO
	huhInitCap                 // ULinda
	huhCap                     // uLinda, fOO
)

// classifyCase reports which case forms a word may stand for.
func classifyCase(word string) caseType {
	var letters, uppers int
	firstUpper := false
	for i, r := range word {
		if !unicode.IsLetter(r) {
			continue
		}
		letters++
		if unicode.IsUpper(r) {
			uppers++
			if i == 0 {
				firstUpper = true
			}
		}
	}
	switch {
	case uppers == 0:
		return noCap
	case uppers == letters:
		return allCap
	case uppers == 1 && firstUpper:
		return initCap
	case firstUpper:
		return huhInitCap
	default:
		return huhCap
	}
}

// lowerFirst lower-cases the first rune of s, leaving the rest unchanged.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// spellDepth is spell with a count of how many BREAK splits led here.
func (s *goSpell) spellDepth(word string, depth int) bool {
	if s.isForbidden(word) {
		return false
	}
	// A trailing period is an abbreviation's, or the sentence's.
	if strings.HasSuffix(word, ".") {
		if t := strings.TrimRight(word, "."); t != "" && s.spellDepth(t, depth) {
			return true
		}
	}
	if _, ok := s.listed[word]; ok {
		return true
	}
	if s.isWord(word, false) {
		return true
	}

	// The forms a word may stand for, as Hunspell reads its capitals.
	lower := s.lowerWord(word)
	if _, ok := s.listed[lower]; ok {
		return true
	}
	switch classifyCase(word) {
	case noCap, huhCap:
		// As written only: fOO is not foo.
	case initCap:
		if !s.dottedFirst(word) && s.isWord(lower, true) {
			return true
		}
	case allCap:
		if s.isWordUpper(word) {
			return true
		}
		// A KEEPCASE word is not reached by folding an all-caps one, and
		// under CHECKSHARPS an all-caps ß word is written with SS.
		forms := []string{lower, capitalize(lower)}
		if s.dottedFirst(word) {
			// İZMİR is İzmir: the first letter keeps its dot.
			forms = []string{"İ" + s.lowerWord(word[len("İ"):])}
		}
		for _, form := range forms {
			for _, f := range s.analyses(form, false) {
				if s.isForbidden(f.Word) || hasFlag(f.Flags, s.affix.CompoundOnly) ||
					hasFlag(f.Flags, s.affix.KeepCaseFlag) || s.hasSharp(f.Word) {
					continue
				}
				return true
			}
		}
	case huhInitCap:
		_, n := utf8.DecodeRuneInString(word)
		if !s.dottedFirst(word) && s.isWord(s.lowerWord(word[:n])+word[n:], true) {
			return true
		}
	}

	if isNumber(word) {
		return true
	}
	if isNumberHex(word) {
		return true
	}

	if isNumberBinary(word) {
		return true
	}

	if isHash(word) {
		return true
	}

	// A COMPOUNDRULE reads the word as written or, for a capitalized or
	// all-caps word, lower-cased: 42ND is 42nd. A compound has two parts at
	// least, so an entry on its own is not one.
	for _, pat := range s.compounds {
		if _, entry := s.roots[word]; !entry && pat.MatchString(word) {
			return true
		}
		if _, entry := s.roots[lower]; !entry && lower != word &&
			classifyCase(word) != huhCap && pat.MatchString(lower) {
			return true
		}
	}

	// Affix-flag compounding (German, Dutch, ...): accept a word that splits
	// into dictionary segments. See #848.
	if s.isCompound(word) {
		return true
	}

	// Maybe a word with units? e.g. 100GB
	if units := isNumberUnits(word); units != "" && s.isWord(units, false) {
		return true
	}

	return s.breakParts(word, depth)
}

// breakParts reports whether word is valid once split by the dictionary's
// BREAK rules: an anchored rule strips its pattern from that end, and any
// other rule splits at each occurrence, with both sides then checked on
// their own (and split again as needed). See #1165.
func (s *goSpell) breakParts(word string, depth int) bool {
	if depth >= maxBreakDepth {
		return false
	}
	for _, r := range s.breaks {
		n := len(r.pattern)
		switch {
		case r.atStart:
			if len(word) > n && strings.HasPrefix(word, r.pattern) &&
				s.spellDepth(word[n:], depth+1) {
				return true
			}
		case r.atEnd:
			if len(word) > n && strings.HasSuffix(word, r.pattern) &&
				s.spellDepth(word[:len(word)-n], depth+1) {
				return true
			}
		default:
			// Only interior occurrences: both sides must be non-empty.
			for i := strings.Index(word[1:], r.pattern); i >= 0; {
				at := i + 1
				if at+n >= len(word) {
					break
				}
				if s.spellDepth(word[:at], depth+1) &&
					s.spellDepth(word[at+n:], depth+1) {
					return true
				}
				next := strings.Index(word[at+1:], r.pattern)
				if next < 0 {
					break
				}
				i = at + next
			}
		}
	}
	return false
}

// segment returns what a compound may use word as. The case is the
// dictionary's to handle: a German dictionary generates the lower-case
// interior forms itself, with prefix rules that strip the capital.
func (s *goSpell) segment(word string) (segment, bool) {
	s.mu.Lock()
	use, cached := s.segments[word]
	s.mu.Unlock()
	if cached {
		return use.seg, use.ok
	}

	var merged segment
	found := false
	if !s.isForbidden(word) {
		for _, f := range s.analyses(word, false) {
			if f.prefix == nil && f.suffix == nil && hasFlag(f.Flags, s.affix.CompoundForbidFlag) {
				// The entry itself is kept out, whatever its other forms allow.
				found = false
				break
			}
			seg, ok := s.affix.compoundUse(f)
			if !ok {
				continue
			}
			seg.keep = hasFlag(f.Flags, s.affix.KeepCaseFlag)
			if found {
				seg = merged.merge(seg)
			}
			merged, found = seg, true
		}
	}
	s.mu.Lock()
	if len(s.segments) > cacheMax {
		s.segments = make(map[string]segmentUse)
	}
	s.segments[word] = segmentUse{seg: merged, ok: found}
	s.mu.Unlock()
	return merged, found
}

// isCompound reports whether word is built from segments the dictionary's
// compound flags allow, for dictionaries that enable flag compounding. The
// word is tried as written and, as Hunspell does, in the case forms a
// capitalized or upper-cased word may stand for.
func (s *goSpell) isCompound(word string) bool {
	if !s.canCompound {
		return false
	}
	if _, ok := s.pairs[word]; ok {
		return false
	}
	// Bound the work: very long inputs are unlikely to be real words and the
	// recursion is super-linear.
	if len([]rune(word)) > 100 {
		return false
	}

	if s.compoundParts([]rune(word), word, false, nil, nil, 0) {
		return true
	}
	// A folded form: a KEEPCASE segment refuses it.
	var forms []string
	lower := strings.ToLower(word)
	switch classifyCase(word) {
	case noCap, huhCap:
		// As written only.
	case allCap:
		forms = []string{lower, capitalize(lower)}
	case initCap:
		forms = []string{lower}
	case huhInitCap:
		forms = []string{lowerFirst(word)}
	}
	for _, form := range forms {
		if s.compoundParts([]rune(form), word, true, nil, nil, 0) {
			return true
		}
	}
	return false
}

// piece is a segment of a compound as written, with what it may do, and
// the compound so far, up to and including it.
type piece struct {
	text  string
	use   segment
	sofar string
}

// compoundParts reports whether runes split into segments allowed at their
// positions. prev is the segment before them; need is the pattern whose
// replacement they follow, if any; word is the whole word, for FORCEUCASE,
// and folded says runes are a case-folded form of it.
func (s *goSpell) compoundParts(runes []rune, word string, folded bool, prev *piece, need *compoundPattern, depth int) bool {
	if depth > 4 { // cap the number of segments
		return false
	}
	// Past COMPOUNDWORDMAX segments, only a short compound goes on: Hungarian
	// allows three parts in six syllables.
	if s.wordMax > 0 && depth+2 > s.wordMax &&
		(s.syllableMax == 0 || s.syllables(word) > s.syllableMax) {
		return false
	}
	minLen := max(s.compoundMin, 1)
	n := len(runes)

	for i := minLen; i <= n-minLen; i++ {
		if !s.boundaryOK(runes, i) {
			continue
		}
		left, rest := string(runes[:i]), runes[i:]

		// With SIMPLIFIEDTRIPLE, `glassko` is glass+sko: the letter the two
		// share is written once.
		candidates := []string{left}
		if s.simplified && runes[i-1] == runes[i] {
			candidates = append(candidates, left+string(runes[i]))
		}
		for _, seg := range candidates {
			if s.compoundFrom(seg, rest, word, folded, prev, need, nil, depth) {
				return true
			}
		}
	}

	// A pattern with a replacement writes the boundary as the replacement:
	// `a/A u/A O` makes sUrya+udayaM into sUryOdayaM.
	for pi := range s.patterns {
		p := &s.patterns[pi]
		if p.repl == "" {
			continue
		}
		repl := []rune(p.repl)
		for j := 1; j+len(repl) < n; j++ {
			if string(runes[j:j+len(repl)]) != p.repl {
				continue
			}
			seg := string(runes[:j]) + p.end
			rest := append([]rune(p.begin), runes[j+len(repl):]...)
			if s.compoundFrom(seg, rest, word, folded, prev, need, p, depth) {
				return true
			}
		}
	}
	return false
}

// compoundFrom reports whether seg, followed by rest, completes a compound.
// need is the pattern seg has to satisfy the right side of; next the one
// whose replacement separates seg from what follows.
func (s *goSpell) compoundFrom(seg string, rest []rune, word string, folded bool, prev *piece, need, next *compoundPattern, depth int) bool {
	use, ok := s.segment(seg)
	first := prev == nil
	if !ok || (first && !use.begin) || (!first && !use.middle) || (folded && use.keep) {
		return false
	}
	if need != nil && need.beginFlag != "" && !hasFlag(use.flags, need.beginFlag) {
		return false
	}
	if next != nil && ((next.endFlag != "" && !hasFlag(use.flags, next.endFlag)) ||
		(next.stemOnly && use.affixed)) {
		return false
	}
	cur := &piece{text: seg, use: use, sofar: seg}
	if prev != nil {
		cur.sofar = prev.sofar + seg
		if s.patternForbids(prev, cur) || s.repMakesWord(prev.text+seg, cur.sofar) {
			return false
		}
	}

	last := string(rest)
	if end, isEnd := s.segment(last); isEnd && end.end && !(folded && end.keep) &&
		!(s.checkDup && last == seg) && (!end.upper || initialUpper(word)) &&
		!s.repMakesWord(seg+last, cur.sofar+last) {
		if next != nil {
			// Hunspell forbids a duplicate only at the end: foofoobar is
			// fine, foobarbar is not.
			return next.beginFlag == "" || hasFlag(end.flags, next.beginFlag)
		}
		if !s.patternForbids(cur, &piece{text: last, use: end}) {
			return true
		}
	}
	return s.compoundParts(rest, word, folded, cur, next, depth+1)
}

// repMakesWord reports whether CHECKCOMPOUNDREP forbids a compound: one REP
// substitution turns a run of its segments into a dictionary word, so the
// compound is more likely a typo of that word.
func (s *goSpell) repMakesWord(texts ...string) bool {
	if !s.checkRep {
		return false
	}
	for _, text := range texts {
		for _, rep := range s.reps {
			for at := strings.Index(text, rep[0]); at >= 0; {
				candidate := text[:at] + rep[1] + text[at+len(rep[0]):]
				if s.isWord(candidate, false) {
					return true
				}
				next := strings.Index(text[at+1:], rep[0])
				if next < 0 {
					break
				}
				at += 1 + next
			}
		}
	}
	return false
}

// syllables counts the COMPOUNDSYLLABLE vowels in word.
func (s *goSpell) syllables(word string) int {
	n := 0
	for _, r := range word {
		if strings.ContainsRune(s.vowels, r) {
			n++
		}
	}
	return n
}

// patternForbids reports whether a CHECKCOMPOUNDPATTERN forbids writing
// left and right together.
func (s *goSpell) patternForbids(left, right *piece) bool {
	for i := range s.patterns {
		p := &s.patterns[i]
		if p.stemOnly && left.use.affixed {
			continue
		}
		if !strings.HasSuffix(left.text, p.end) || !strings.HasPrefix(right.text, p.begin) {
			continue
		}
		if (p.endFlag != "" && !hasFlag(left.use.flags, p.endFlag)) ||
			(p.beginFlag != "" && !hasFlag(right.use.flags, p.beginFlag)) {
			continue
		}
		return true
	}
	return false
}

// boundaryOK applies the checks a dictionary asks for at a compound boundary
// before runes[i], as the word is written.
func (s *goSpell) boundaryOK(runes []rune, i int) bool {
	before, after := runes[i-1], runes[i]
	if s.checkTriple {
		if (i >= 2 && runes[i-2] == before && before == after) ||
			(i+1 < len(runes) && before == after && after == runes[i+1]) {
			return false
		}
	}
	if s.checkCase && before != '-' && after != '-' &&
		(unicode.IsUpper(before) || unicode.IsUpper(after)) {
		return false
	}
	return true
}

// isMorphField reports whether a `.dic` field is morphology, `po:noun`.
func isMorphField(field string) bool {
	return len(field) > 2 && field[2] == ':' &&
		unicode.IsLetter(rune(field[0])) && unicode.IsLetter(rune(field[1]))
}

// capitalize upper-cases the first rune of s, leaving the rest unchanged.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// newGoSpellReader creates a speller from io.Readers for
// Hunspell files
func newGoSpellReader(aff, dic io.Reader) (*goSpell, error) {
	affBytes, err := io.ReadAll(aff)
	if err != nil {
		return nil, err
	}
	dicBytes, err := io.ReadAll(dic)
	if err != nil {
		return nil, err
	}
	affBytes, dicBytes, err = decodeDictionary(affBytes, dicBytes)
	if err != nil {
		return nil, err
	}

	affix, err := newDictConfig(bytes.NewReader(affBytes))
	if err != nil {
		return nil, err
	}
	dic = bytes.NewReader(dicBytes)

	scanner := bufio.NewScanner(dic)
	// get first line
	if !scanner.Scan() {
		return nil, scanner.Err()
	}
	// The first line is the entry count, which sizes the roots up front.
	size := 0
	if fields := strings.Fields(scanner.Text()); len(fields) > 0 {
		size, _ = strconv.Atoi(fields[0])
	}
	size = min(max(size, 0), 1<<20)

	gs := goSpell{
		dictData: &dictData{
			affix:          affix,
			roots:          make(map[string][]rootEntry, size),
			upperRoots:     make(map[string][]string, size/4),
			forbiddenRoots: make(map[string][]rootEntry),
			pairs:          make(map[string]struct{}),
			compounds:      make([]*regexp.Regexp, 0, len(affix.CompoundRule)),
			splitter:       newSplitter(affix.WordChars),
			canCompound:    affix.compoundingEnabled(),
			compoundMin:    affix.CompoundMin,
			checkDup:       affix.CheckCompoundDup,
			checkTriple:    affix.CheckCompoundTriple,
			simplified:     affix.SimplifiedTriple,
			checkCase:      affix.CheckCompoundCase,
			checkRep:       affix.CheckCompoundRep,
			wordMax:        affix.CompoundWordMax,
			syllableMax:    affix.CompoundSyllable,
			vowels:         affix.CompoundVowels,
			reps:           affix.Replacements,
			patterns:       affix.CompoundPatterns,
			breaks:         newBreakRules(affix.Break),
			ignored:        affix.Ignored,
		},
		listed:   make(map[string]struct{}),
		spelled:  make(map[string]bool),
		segments: make(map[string]segmentUse),
	}
	if !affix.BreakDeclared {
		gs.breaks = newBreakRules(defaultBreaks)
	}

	for scanner.Scan() {
		line := scanner.Text()
		// A .dic entry is `word/flags` optionally followed by whitespace-
		// separated morphological fields, e.g.
		//
		//	abandonware/M	Noun: uncountable
		//	coitus/10,39,31 al:coituum
		//
		// Keep only the first field; otherwise the morphology corrupts flag
		// parsing (e.g., FLAG num would read "31 al:coituum" as a flag).
		//
		// Both tab- and space-separated morphology occur in the wild -- the
		// Danish dictionary from stavekontrolden.dk uses spaces. See #1065.
		//
		// The word ends at the first tab; morphology follows it.
		line, _, _ = strings.Cut(line, "\t")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		line = fields[0]

		// A space inside the word makes a pair, `compound word`, whose
		// run-together form is not a compound.
		if len(fields) > 1 && !isMorphField(fields[1]) {
			gs.pairs[fields[0]+fields[1]] = struct{}{}
			gs.addRoot(fields[0]+" "+fields[1], nil)
			continue
		}

		word, keyString, found := splitEntry(line)
		if word == "" || (found && keyString == "") {
			// Skip malformed entries (e.g., a line with flags but no word)
			// rather than abandoning the entire dictionary, which would leave
			// every word unrecognized and flagged. See #1065.
			continue
		}
		var flags []string
		if found {
			flags = affix.parseFlags(keyString)
		}

		if hasFlag(flags, affix.ForbiddenFlag) {
			word = affix.ignore(word)
			gs.forbiddenRoots[word] = append(gs.forbiddenRoots[word], rootEntry{flags: flags})
			gs.noteUpper(word)
			continue
		}

		for _, key := range flags {
			if _, ok := affix.compoundMap[key]; ok {
				affix.compoundMap[key] = append(affix.compoundMap[key], word)
			}
		}
		gs.addRoot(word, flags)
	}

	if err = scanner.Err(); err != nil {
		return nil, err
	}

	for _, compoundRule := range affix.CompoundRule {
		// Each flag becomes a group of the words that carry it, and `*`
		// and `?` apply to the group before them.
		pattern := "^"
		for _, tok := range affix.compoundRuleTokens(compoundRule) {
			if tok.op != "" {
				pattern += tok.op
				continue
			}
			words := affix.compoundMap[tok.flag]
			if len(words) == 0 {
				pattern += `([^\s\S])` // nothing carries the flag
				continue
			}
			quoted := make([]string, len(words))
			for i, w := range words {
				quoted[i] = regexp.QuoteMeta(w)
			}
			pattern += "(" + strings.Join(quoted, "|") + ")"
		}
		pattern += "$"

		pat, perr := regexp.Compile(pattern)
		if perr != nil {
			return nil, perr
		}
		gs.compounds = append(gs.compounds, pat)
	}

	if len(affix.IconvReplacements) > 0 {
		// Hunspell converts by the longest matching rule; the replacer
		// takes the first listed, so the longest are listed first.
		pairs := make([][2]string, 0, len(affix.IconvReplacements)/2)
		for i := 0; i+1 < len(affix.IconvReplacements); i += 2 {
			pairs = append(pairs, [2]string{affix.IconvReplacements[i], affix.IconvReplacements[i+1]})
		}
		sort.SliceStable(pairs, func(i, j int) bool { return len(pairs[i][0]) > len(pairs[j][0]) })
		flat := make([]string, 0, 2*len(pairs))
		for _, p := range pairs {
			flat = append(flat, p[0], p[1])
		}
		gs.ireplacer = strings.NewReplacer(flat...)
	}
	return &gs, nil
}

// splitEntry divides a `.dic` entry into its word and flags. A `\/` is a
// slash in the word, and so is a slash that starts it.
func splitEntry(entry string) (string, string, bool) {
	const escaped = "\uffff"
	entry = strings.ReplaceAll(entry, `\/`, escaped)
	at := strings.Index(entry[1:], "/")
	if at < 0 {
		return strings.ReplaceAll(entry, escaped, "/"), "", false
	}
	word, flags := entry[:at+1], entry[at+2:]
	return strings.ReplaceAll(word, escaped, "/"), flags, true
}

// addRoot records a `.dic` entry.
func (s *goSpell) addRoot(word string, flags []string) {
	word = s.affix.ignore(word)
	s.roots[word] = append(s.roots[word], rootEntry{flags: flags})
	s.noteUpper(word)
}

// noteUpper indexes a root with a capital, or a ß, by its upper-cased form.
func (s *goSpell) noteUpper(word string) {
	if word == strings.ToLower(word) && !s.hasSharp(word) {
		return
	}
	upper := s.upperKey(word)
	if !stringIn(word, s.upperRoots[upper]) {
		s.upperRoots[upper] = append(s.upperRoots[upper], word)
	}
}

// upperKey upper-cases a word as an all-caps writer would: under
// CHECKSHARPS, ß and ẞ become SS; in a Turkic language, i becomes İ and ı
// becomes I.
func (s *goSpell) upperKey(word string) string {
	if s.turkic() {
		word = strings.NewReplacer("i", "İ", "ı", "I").Replace(word)
	}
	upper := strings.ToUpper(word)
	if s.affix.CheckSharps {
		upper = strings.NewReplacer("ß", "SS", "ẞ", "SS").Replace(upper)
	}
	return upper
}

// turkic reports whether LANG names a language with the dotted and dotless i.
func (s *goSpell) turkic() bool {
	lang := strings.ToLower(s.affix.Lang)
	return strings.HasPrefix(lang, "tr") || strings.HasPrefix(lang, "az") || strings.HasPrefix(lang, "crh")
}

// lowerWord lower-cases a word: in a Turkic language, İ becomes i and I
// becomes ı.
func (s *goSpell) lowerWord(word string) string {
	if s.turkic() {
		word = strings.NewReplacer("İ", "i", "I", "ı").Replace(word)
	}
	return strings.ToLower(word)
}

// dottedFirst reports whether word starts with İ outside a Turkic language,
// where that letter has no lower-case form to fold to.
func (s *goSpell) dottedFirst(word string) bool {
	return !s.turkic() && strings.HasPrefix(word, "İ")
}

// hasSharp reports whether word holds a sharp s, in either case.
func (s *goSpell) hasSharp(word string) bool {
	return s.affix.CheckSharps && strings.ContainsAny(word, "ßẞ")
}

// recase returns the one spelling the dictionary has for word in another
// case, such as GitHub for github. Two spellings that differ in case, or
// none, return false.
func (s *goSpell) recase(word string) (string, bool) {
	found := ""
	for _, f := range s.analyses(s.upperKey(word), true) {
		if f.Word == word || s.isForbidden(f.Word) || !s.spell(f.Word) {
			continue
		}
		if found != "" && found != f.Word {
			return "", false
		}
		found = f.Word
	}
	return found, found != ""
}
