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
	dict map[string]struct{}

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

	matches := []wordMatch{}
	for _, option := range s.keys() {
		sim := strutil.Similarity(option, word, metric)
		matches = append(matches, wordMatch{option, sim})
	}

	sort.Slice(matches, func(i, j int) bool {
		return matches[i].score > matches[j].score
	})

	hits := matches[:5]
	if word == strings.Title(word) { //nolint:staticcheck
		// Capitalized word, so capitalize the suggestions
		for i := range hits {
			hits[i].word = strings.Title(hits[i].word) //nolint:staticcheck
		}
	}

	return hits
}

// spell checks to see if a given word is in the internal dictionaries
func (s *goSpell) spell(word string) bool {
	_, ok := s.dict[word]
	if ok {
		return true
	}
	_, ok = s.dict[strings.ToLower(word)]
	if ok {
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

	// Affix-flag compounding (German, Dutch, ...): accept a word that splits
	// into dictionary segments. See #848.
	if s.isCompound(word) {
		return true
	}

	// Maybe a word with units? e.g. 100GB
	units := isNumberUnits(word)
	if units != "" {
		// dictionary appears to have list of units
		if _, ok = s.dict[units]; ok {
			return true
		}
	}

	return false
}

// inDict reports whether word is a dictionary entry, trying its exact,
// lower-cased, and title-cased forms. The latter two matter for compound
// segments: e.g. a German compound writes interior nouns lower-case, while the
// dictionary stores them capitalized.
func (s *goSpell) inDict(word string) bool {
	if _, ok := s.dict[word]; ok {
		return true
	}
	if _, ok := s.dict[strings.ToLower(word)]; ok {
		return true
	}
	if _, ok := s.dict[capitalize(word)]; ok {
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
	minLen := s.compoundMin
	if minLen < 1 {
		minLen = 1
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
		dict:        make(map[string]struct{}),
		compounds:   make([]*regexp.Regexp, 0, len(affix.CompoundRule)),
		splitter:    newSplitter(affix.WordChars),
		canCompound: affix.compoundingEnabled(),
		compoundMin: int(affix.CompoundMin),
	}

	words := []string{}
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

		words, err = affix.expand(line, words)
		if err != nil {
			// Skip malformed entries (e.g., a line with flags but no word)
			// rather than abandoning the entire dictionary, which would leave
			// every word unrecognized and flagged. See #1065.
			continue
		}

		if len(words) == 0 {
			continue
		}

		for _, word := range words {
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
