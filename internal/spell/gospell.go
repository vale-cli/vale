package spell

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/adrg/strutil"
	"github.com/adrg/strutil/metrics"
)

type wordMatch struct {
	word  string
	score float64
}

type goSpell struct {
	dict               map[string]struct{}
	roots              map[string][]dictionaryFlags
	affix              *dictConfig
	reverse            reverseAffixIndex
	cache              *spellCache
	compoundFlagCache  *spellCache
	compoundMatchCache *spellCache
	lazyCompoundRules  bool

	ireplacer   *strings.Replacer
	compounds   []*regexp.Regexp
	splitter    *splitter
	canCompound bool // dictionary uses COMPOUNDFLAG/BEGIN/MIDDLE/END
	compoundMin int
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
	_, ok := s.dict[word]
	if ok {
		// already exists
		return false
	}
	s.dict[word] = struct{}{}
	return true
}

// addWordListFile reads in a word list file
func (s *goSpell) addWordListFile(name string) ([]string, error) {
	fd, err := os.Open(name)
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
	keys := make([]string, len(s.dict))

	i := 0
	for k := range s.dict {
		keys[i] = k
		i++
	}

	return keys
}

func (s *goSpell) suggest(word string) []wordMatch {
	metric := metrics.NewLevenshtein()
	matches := make([]wordMatch, 0, maxSuggestionMatches)
	seen := make(map[string]struct{})
	checked := make(map[string]struct{})
	checks := 0
	consider := func(candidate string) bool {
		if candidate == word {
			return true
		}
		if _, found := checked[candidate]; found {
			return true
		}
		if checks >= maxSuggestionChecks || len(matches) >= maxSuggestionMatches {
			return false
		}
		checked[candidate] = struct{}{}
		checks++
		if s.inLexicon(candidate) || s.inLexicon(strings.ToLower(candidate)) {
			seen[candidate] = struct{}{}
			matches = append(matches, wordMatch{
				word:  candidate,
				score: strutil.Similarity(candidate, word, metric),
			})
		}
		return true
	}

	runes := []rune(word)
mutationLoop:
	for i := range runes {
		candidate := string(append(append([]rune{}, runes[:i]...), runes[i+1:]...))
		if !consider(candidate) {
			break mutationLoop
		}
		if i+1 < len(runes) {
			swapped := append([]rune{}, runes...)
			swapped[i], swapped[i+1] = swapped[i+1], swapped[i]
			if !consider(string(swapped)) {
				break mutationLoop
			}
		}
	}

	tryChars := uniqueRunes(s.affix.TryChars)
	for i := 0; i < len(runes) && checks < maxSuggestionChecks && len(matches) < maxSuggestionMatches; i++ {
		for _, char := range tryChars {
			replaced := append([]rune{}, runes...)
			replaced[i] = char
			if !consider(string(replaced)) {
				break
			}
		}
	}
	for i := 0; i <= len(runes) && checks < maxSuggestionChecks && len(matches) < maxSuggestionMatches; i++ {
		for _, char := range tryChars {
			inserted := make([]rune, 0, len(runes)+1)
			inserted = append(inserted, runes[:i]...)
			inserted = append(inserted, char)
			inserted = append(inserted, runes[i:]...)
			if !consider(string(inserted)) {
				break
			}
		}
	}

	// Mutations preserve derived suggestions without materializing every
	// surface. Roots remain a bounded-memory fallback for more distant typos.
	if len(matches) < 5 {
		for _, option := range s.keys() {
			if _, found := seen[option]; found {
				continue
			}
			matches = append(matches, wordMatch{
				word:  option,
				score: strutil.Similarity(option, word, metric),
			})
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].score == matches[j].score {
			return matches[i].word < matches[j].word
		}
		return matches[i].score > matches[j].score
	})

	hits := matches[:min(5, len(matches))]
	if word == strings.Title(word) { //nolint:staticcheck
		// Capitalized word, so capitalize the suggestions
		for i := range hits {
			hits[i].word = strings.Title(hits[i].word) //nolint:staticcheck
		}
	}

	return hits
}

const (
	maxSuggestionChecks  = 4096
	maxSuggestionMatches = 256
)

// uniqueRunes returns the runes in text once each, preserving first-seen order.
func uniqueRunes(text string) []rune {
	seen := make(map[rune]struct{})
	result := make([]rune, 0, len(text))
	for _, char := range text {
		if _, found := seen[char]; found {
			continue
		}
		seen[char] = struct{}{}
		result = append(result, char)
	}
	return result
}

// spell checks to see if a given word is in the internal dictionaries
func (s *goSpell) spell(word string) bool {
	if s.inLexicon(word) {
		return true
	}
	if s.inLexicon(strings.ToLower(word)) {
		return true
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

	// check compounds
	for _, pat := range s.compounds {
		if pat.MatchString(word) {
			return true
		}
	}
	if s.matchesLazyCompoundRule(word) {
		return true
	}

	// Affix-flag compounding (German, Dutch, ...): accept a word that splits
	// into dictionary segments. See #848.
	if s.isCompound(word) {
		return true
	}

	// Maybe a word with units? e.g. 100GB
	units := isNumberUnits(word)
	if units != "" {
		// dictionary appears to have list of units
		if s.inLexicon(units) {
			return true
		}
	}

	return false
}

// inLexicon reports whether word is an explicit dictionary entry or can be
// derived through affixes, caching derived lookups.
func (s *goSpell) inLexicon(word string) bool {
	if _, ok := s.dict[word]; ok {
		return true
	}
	if value, found := s.cache.get(word); found {
		return value
	}
	value := s.isDerived(word)
	s.cache.set(word, value)
	return value
}

// isDerived reports whether an indexed dictionary root can generate target.
func (s *goSpell) isDerived(target string) bool {
	return s.matchesRoot(target, func(
		root string,
		entry dictionaryFlags,
		allowed map[string]struct{},
	) bool {
		return s.affix.expandsToWithin(root, entry.text, target, allowed)
	})
}

// matchesRoot walks reverse-affix candidates for target and invokes match for
// every dictionary root found along the way.
func (s *goSpell) matchesRoot(
	target string,
	match func(string, dictionaryFlags, map[string]struct{}) bool,
) bool {
	current := map[string]struct{}{target: {}}
	tested := make(map[string]struct{})
	for depth := 0; depth <= maxReverseAffixes; depth++ {
		next := make(map[string]struct{})
		for candidate := range current {
			if _, seen := tested[candidate]; !seen {
				tested[candidate] = struct{}{}
				for _, entry := range s.roots[candidate] {
					if match(candidate, entry, tested) {
						return true
					}
				}
			}
			if depth < maxReverseAffixes {
				s.reverse.predecessors(candidate, next)
			}
		}
		current = next
	}
	return false
}

// hasCompoundFlag reports whether word can be generated in a state carrying
// flag, caching results separately for each word-and-flag pair.
func (s *goSpell) hasCompoundFlag(word, flag string) bool {
	cacheKey := flag + "\x00" + word
	if value, found := s.compoundFlagCache.get(cacheKey); found {
		return value
	}
	value := s.matchesRoot(word, func(
		root string,
		entry dictionaryFlags,
		allowed map[string]struct{},
	) bool {
		return s.affix.hasFlaggedFormWithin(root, entry.text, word, flag, allowed)
	})
	s.compoundFlagCache.set(cacheKey, value)
	return value
}

// matchesLazyCompoundRule reports whether word satisfies any COMPOUNDRULE that
// depends on lazily generated affix forms.
func (s *goSpell) matchesLazyCompoundRule(word string) bool {
	if !s.lazyCompoundRules {
		return false
	}
	if value, found := s.compoundMatchCache.get(word); found {
		return value
	}

	boundaries := []int{0}
	for index := range word {
		if index > 0 {
			boundaries = append(boundaries, index)
		}
	}
	boundaries = append(boundaries, len(word))
	for _, compoundRule := range s.affix.CompoundRule {
		var pattern strings.Builder
		pattern.WriteByte('^')
		for _, flag := range s.affix.parseFlags(compoundRule) {
			if len(flag) == 1 && strings.ContainsRune("()+?*", rune(flag[0])) {
				// Preserve the existing COMPOUNDRULE regexp construction: these
				// characters are treated as literal rule tokens by Vale.
				pattern.WriteString(regexp.QuoteMeta(flag))
				continue
			}

			alternatives := make(map[string]struct{})
			for start := 0; start+1 < len(boundaries); start++ {
				for end := start + 1; end < len(boundaries); end++ {
					part := word[boundaries[start]:boundaries[end]]
					if s.hasCompoundFlag(part, flag) {
						alternatives[part] = struct{}{}
					}
				}
			}
			pattern.WriteString("(?:")
			if len(alternatives) == 0 {
				// A NUL cannot occur in a token produced by Vale's splitters.
				pattern.WriteString(`\x00`)
			} else {
				parts := make([]string, 0, len(alternatives))
				for part := range alternatives {
					parts = append(parts, part)
				}
				sort.Slice(parts, func(i, j int) bool {
					return len(parts[i]) > len(parts[j])
				})
				for index, part := range parts {
					if index > 0 {
						pattern.WriteByte('|')
					}
					pattern.WriteString(regexp.QuoteMeta(part))
				}
			}
			pattern.WriteByte(')')
		}
		pattern.WriteByte('$')
		compiled, err := regexp.Compile(pattern.String())
		if err == nil && compiled.MatchString(word) {
			s.compoundMatchCache.set(word, true)
			return true
		}
	}
	s.compoundMatchCache.set(word, false)
	return false
}

// hasLazyCompoundForms reports whether an affix continuation can attach a flag
// referenced by a COMPOUNDRULE.
func hasLazyCompoundForms(affix *dictConfig) bool {
	if len(affix.CompoundRule) == 0 {
		return false
	}
	for _, class := range affix.AffixMap {
		for _, current := range class.Rules {
			continuation := affix.resolveFlagAlias(current.Cont)
			for _, flag := range affix.parseFlags(continuation) {
				if _, found := affix.compoundMap[flag]; found {
					return true
				}
			}
		}
	}
	return false
}

// inDict reports whether word is a dictionary entry, trying its exact,
// lower-cased, and title-cased forms. The latter two matter for compound
// segments: e.g. a German compound writes interior nouns lower-case, while the
// dictionary stores them capitalized.
func (s *goSpell) inDict(word string) bool {
	if s.inLexicon(word) {
		return true
	}
	if s.inLexicon(strings.ToLower(word)) {
		return true
	}
	if s.inLexicon(capitalize(word)) {
		return true
	}
	return false
}

// isCompound reports whether word can be segmented into dictionary words, for
// dictionaries that enable affix-flag compounding. This is an approximation of
// Hunspell's COMPOUNDFLAG/BEGIN/MIDDLE/END handling: it doesn't verify each
// segment's position flags, but recognizing legitimate compounds (rather than
// flagging them) is the priority. See #848.
func (s *goSpell) isCompound(word string) bool {
	if !s.canCompound {
		return false
	}
	// Bound the work: very long inputs are unlikely to be real words and the
	// recursion is super-linear.
	if r := []rune(word); len(r) <= 100 {
		return s.compoundParts(r, 0)
	}
	return false
}

func (s *goSpell) compoundParts(runes []rune, depth int) bool {
	if depth > 4 { // cap the number of segments
		return false
	}
	// Enforce a sane minimum segment length. Some dictionaries set
	// COMPOUNDMIN very low (OpenTaal's Dutch uses 0) and rely on per-segment
	// position flags -- which this approximation doesn't check -- to constrain
	// compounds. Without a floor, every short letter string would split into
	// 1-2 char dictionary entries and be wrongly accepted. See #776.
	minLen := s.compoundMin
	if minLen < 3 {
		minLen = 3
	}
	n := len(runes)
	for i := minLen; i <= n-minLen; i++ {
		if s.inDict(string(runes[:i])) &&
			(s.inDict(string(runes[i:])) || s.compoundParts(runes[i:], depth+1)) {
			return true
		}
	}
	return false
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
	affix, err := newDictConfig(aff)
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(dic)
	// get first line
	if !scanner.Scan() {
		return nil, scanner.Err()
	}

	gs := goSpell{
		// TODO: Use fixed size from first list?
		dict:               make(map[string]struct{}),
		roots:              make(map[string][]dictionaryFlags),
		affix:              affix,
		reverse:            newReverseAffixIndex(affix.AffixMap),
		cache:              newSpellCache(),
		compoundFlagCache:  newSpellCache(),
		compoundMatchCache: newSpellCache(),
		lazyCompoundRules:  hasLazyCompoundForms(affix),
		compounds:          make([]*regexp.Regexp, 0, len(affix.CompoundRule)),
		splitter:           newSplitter(affix.WordChars),
		canCompound:        affix.compoundingEnabled(),
		compoundMin:        affix.CompoundMin,
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
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		line = fields[0]

		word, flags, entryErr := affix.dictionaryEntry(line)
		err = entryErr
		if err != nil {
			// Skip malformed entries (e.g., a line with flags but no word)
			// rather than abandoning the entire dictionary, which would leave
			// every word unrecognized and flagged. See #1065.
			continue
		}

		gs.roots[word] = append(gs.roots[word], dictionaryFlags{text: flags})
		compoundOnly := false
		for _, flag := range affix.parseFlags(flags) {
			if flag == affix.CompoundOnly {
				compoundOnly = true
			}
			if _, found := affix.compoundMap[flag]; found {
				affix.compoundMap[flag] = append(affix.compoundMap[flag], word)
			}
		}
		if !compoundOnly {
			gs.dict[word] = struct{}{}
		}
	}

	if err = scanner.Err(); err != nil {
		return nil, err
	}

	for _, compoundRule := range affix.CompoundRule {
		pattern := "^"
		for _, key := range affix.parseFlags(compoundRule) {
			if len(key) == 1 {
				r := rune(key[0])
				switch r {
				case '(', ')', '+', '?', '*':
					pattern += regexp.QuoteMeta(key)
					continue
				}
			}
			groups := affix.compoundMap[key]
			pattern = pattern + "(" + strings.Join(groups, "|") + ")"
		}
		pattern += "$"

		pat, perr := regexp.Compile(pattern)
		if perr != nil {
			return nil, perr
		}
		gs.compounds = append(gs.compounds, pat)
	}

	if len(affix.IconvReplacements) > 0 {
		gs.ireplacer = strings.NewReplacer(affix.IconvReplacements...)
	}
	return &gs, nil
}

// newGoSpell from AFF and DIC Hunspell filenames
func newGoSpell(affFile, dicFile string) (*goSpell, error) {
	aff, err := os.Open(affFile)
	if err != nil {
		return nil, fmt.Errorf("unable to open aff: %s", err.Error())
	}
	defer aff.Close()
	dic, err := os.Open(dicFile)
	if err != nil {
		return nil, fmt.Errorf("unable to open dic: %s", err.Error())
	}
	defer dic.Close()
	h, err := newGoSpellReader(aff, dic)
	return h, err
}
