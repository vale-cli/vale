package spell

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/adrg/strutil"
	"github.com/adrg/strutil/metrics"
)

type wordMatch struct {
	word  string
	score float64
}

type goSpell struct {
	dict map[string]struct{}

	ireplacer *strings.Replacer
	compounds []*regexp.Regexp
	splitter  *splitter
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
		dict:      make(map[string]struct{}),
		compounds: make([]*regexp.Regexp, 0, len(affix.CompoundRule)),
		splitter:  newSplitter(affix.WordChars),
	}

	words := []string{}
	for scanner.Scan() {
		line := scanner.Text()
		// NOTE: We do this for entries like
		//
		// abandonware/M	Noun: uncountable
		line = strings.Split(line, "\t")[0]

		words, err = affix.expand(line, words)
		if err != nil {
			return nil, fmt.Errorf("unable to process %q: %s", line, err.Error())
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
