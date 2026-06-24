package spell

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// affixType is either an affix prefix or suffix
type affixType int

// specific Affix types
const (
	Prefix affixType = iota
	Suffix
)

// affix is a rule for affix (adding prefixes or suffixes)
type affix struct {
	Rules        []rule    // -
	Type         affixType // either PFX or SFX
	CrossProduct bool      // -
}

// expand provides all variations of a given word based on this affix rule
func (a affix) expand(word string, out []string) []string {
	for _, r := range a.Rules {
		if r.matcher != nil && !r.matcher.MatchString(word) {
			continue
		}
		if a.Type == Prefix {
			out = append(out, r.AffixText+word)
			// TODO is does Strip apply to prefixes too?
		} else {
			stripWord := word
			if r.Strip != "" && strings.HasSuffix(word, r.Strip) {
				stripWord = word[:len(word)-len(r.Strip)]
			}
			out = append(out, stripWord+r.AffixText)
		}
	}
	return out
}

// rule is a Affix rule
type rule struct {
	Strip     string
	AffixText string         // suffix or prefix text to add
	Pattern   string         // original matching pattern from AFF file
	matcher   *regexp.Regexp // matcher to see if this rule applies or not
}

// dictConfig is a partial representation of a Hunspell AFF (Affix) file.
type dictConfig struct {
	IconvReplacements []string
	Replacements      [][2]string
	CompoundRule      []string
	Flag              string
	TryChars          string
	WordChars         string
	CompoundOnly      string
	CompoundFlag      string
	CompoundBegin     string
	CompoundMiddle    string
	CompoundEnd       string
	AffixMap          map[string]affix
	CamelCase         int
	CompoundMin       int64
	compoundMap       map[string][]string
	NoSuggestFlag     string
}

// compoundingEnabled reports whether the dictionary uses affix-flag-based
// compounding (COMPOUNDFLAG / COMPOUNDBEGIN / MIDDLE / END), as German, Dutch,
// etc. do. COMPOUNDRULE is handled separately. See #848.
func (a *dictConfig) compoundingEnabled() bool {
	return a.CompoundFlag != "" || a.CompoundBegin != "" ||
		a.CompoundMiddle != "" || a.CompoundEnd != ""
}

// parseFlags splits a flag string into individual flags based on the FLAG type.
//
// Hunspell supports several flag formats:
//   - "ASCII" (default): each character is a flag
//   - "num": flags are comma-separated numbers (e.g., "14308,10482,4720")
//   - "UTF-8": each UTF-8 character is a flag
//   - "long": each pair of ASCII characters is a flag
func (a dictConfig) parseFlags(flagStr string) []string {
	switch a.Flag {
	case "num":
		return strings.Split(flagStr, ",")
	case "long":
		flags := make([]string, 0, len(flagStr)/2)
		for i := 0; i+1 < len(flagStr); i += 2 {
			flags = append(flags, flagStr[i:i+2])
		}
		return flags
	default: // "ASCII" or "UTF-8"
		flags := make([]string, 0, len(flagStr))
		for _, r := range flagStr {
			flags = append(flags, string(r))
		}
		return flags
	}
}

// expand expands a word/affix using dictionary/affix rules
//
//	This also supports CompoundRule flags
func (a dictConfig) expand(wordAffix string, out []string) ([]string, error) {
	out = out[:0]
	idx := strings.Index(wordAffix, "/")

	// not found
	if idx == -1 {
		out = append(out, wordAffix)
		return out, nil
	}
	if idx == 0 || idx+1 == len(wordAffix) {
		return nil, fmt.Errorf("slash char found in first or last position")
	}
	// safe
	word, keyString := wordAffix[:idx], wordAffix[idx+1:]

	flags := a.parseFlags(keyString)

	// check to see if any of the flags are in the
	// "compound only".  If so then nothing to add
	compoundOnly := false
	for _, key := range flags {
		if key == a.CompoundOnly {
			compoundOnly = true
			continue
		}
		if _, ok := a.compoundMap[key]; !ok {
			// the isn't a compound flag
			continue
		}
		// is a compound flag
		a.compoundMap[key] = append(a.compoundMap[key], word)
	}

	if compoundOnly {
		return out, nil
	}

	out = append(out, word)
	prefixes := make([]affix, 0, 5)
	suffixes := make([]affix, 0, 5)
	for _, key := range flags {
		af, ok := a.AffixMap[key]
		if !ok {
			continue
		}
		if !af.CrossProduct {
			out = af.expand(word, out)
			continue
		}
		if af.Type == Prefix {
			prefixes = append(prefixes, af)
		} else {
			suffixes = append(suffixes, af)
		}
	}

	// expand all suffixes with out any prefixes
	for _, suf := range suffixes {
		out = suf.expand(word, out)
	}
	for _, pre := range prefixes {
		prewords := pre.expand(word, nil)
		out = append(out, prewords...)

		// now do cross product
		for _, suf := range suffixes {
			for _, w := range prewords {
				out = suf.expand(w, out)
			}
		}
	}
	return out, nil
}

// allDigits reports whether s is non-empty and contains only ASCII digits. It
// distinguishes a PFX/SFX header's count field from a rule's affix text when
// both lines have four fields. See #776.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isCrossProduct(val string) (bool, error) {
	switch val {
	case "Y":
		return true, nil
	case "N":
		return false, nil
	}
	return false, fmt.Errorf("CrossProduct is not Y or N: got %q", val)
}

// newDictConfig reads an Hunspell AFF file
func newDictConfig(file io.Reader) (*dictConfig, error) { //nolint:funlen
	aff := dictConfig{
		Flag:        "ASCII",
		AffixMap:    make(map[string]affix),
		compoundMap: make(map[string][]string),
		CompoundMin: 3, // default in Hunspell
	}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		switch parts[0] {
		case "TRY":
			if len(parts) < 2 {
				return nil, fmt.Errorf("TRY stanza had %d fields, expected 2", len(parts))
			}
			aff.TryChars = parts[1]
		case "ICONV":
			// if only 2 fields, then its the first stanza that just provides a count
			//  we don't care, as we dynamically allocate
			if len(parts) == 2 {
				continue
			} else if len(parts) < 3 {
				return nil, fmt.Errorf("ICONV stanza had %d fields, expected 2", len(parts))
			}
			aff.IconvReplacements = append(aff.IconvReplacements, parts[1], parts[2])
		case "REP":
			if len(parts) == 2 {
				continue
			} else if len(parts) < 3 {
				return nil, fmt.Errorf("REP stanza had %d fields, expected 2", len(parts))
			}
			aff.Replacements = append(aff.Replacements, [2]string{parts[1], parts[2]})
		case "COMPOUNDMIN":
			if len(parts) < 2 {
				return nil, fmt.Errorf("COMPOUNDMIN stanza had %d fields, expected 2", len(parts))
			}
			val, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("COMPOUNDMIN stanza had %q expected number", parts[1])
			}
			aff.CompoundMin = val
		case "ONLYINCOMPOUND":
			if len(parts) < 2 {
				return nil, fmt.Errorf("ONLYINCOMPOUND stanza had %d fields, expected 2", len(parts))
			}
			aff.CompoundOnly = parts[1]
		case "COMPOUNDRULE":
			if len(parts) < 2 {
				return nil, fmt.Errorf("COMPOUNDRULE stanza had %d fields, expected 2", len(parts))
			}
			val, err := strconv.ParseInt(parts[1], 10, 64)
			if err == nil {
				aff.CompoundRule = make([]string, 0, val)
			} else {
				aff.CompoundRule = append(aff.CompoundRule, parts[1])
				for _, flag := range aff.parseFlags(parts[1]) {
					if _, ok := aff.compoundMap[flag]; !ok {
						aff.compoundMap[flag] = []string{}
					}
				}
			}
		case "NOSUGGEST":
			if len(parts) < 2 {
				return nil, fmt.Errorf("NOSUGGEST stanza had %d fields, expected 2", len(parts))
			}
			aff.NoSuggestFlag = parts[1]
		case "COMPOUNDFLAG":
			if len(parts) >= 2 {
				aff.CompoundFlag = parts[1]
			}
		case "COMPOUNDBEGIN":
			if len(parts) >= 2 {
				aff.CompoundBegin = parts[1]
			}
		case "COMPOUNDMIDDLE":
			if len(parts) >= 2 {
				aff.CompoundMiddle = parts[1]
			}
		case "COMPOUNDEND":
			if len(parts) >= 2 {
				aff.CompoundEnd = parts[1]
			}
		case "WORDCHARS":
			if len(parts) < 2 {
				return nil, fmt.Errorf("WORDCHAR stanza had %d fields, expected 2", len(parts))
			}
			aff.WordChars = parts[1]
		case "FLAG":
			if len(parts) < 2 {
				return nil, fmt.Errorf("FLAG stanza had %d, expected 1", len(parts))
			}
			aff.Flag = parts[1]
		case "PFX", "SFX":
			atype := Prefix
			if parts[0] == "SFX" {
				atype = Suffix
			}

			sections := len(parts)
			// A header line is `PFX/SFX flag Y|N count`; a rule line is
			// `PFX/SFX flag strip affix [condition]`. They can both have four
			// fields -- some dictionaries (e.g. OpenTaal's Dutch) omit the
			// rule's condition -- so distinguish by the cross-product flag
			// rather than by field count alone. See #776.
			isHeader := sections == 4 &&
				(parts[2] == "Y" || parts[2] == "N") && allDigits(parts[3])
			switch {
			case isHeader:
				cross, err := isCrossProduct(parts[2])
				if err != nil {
					return nil, err
				}
				// this is a new Affix!
				aff.AffixMap[parts[1]] = affix{
					Type:         atype,
					CrossProduct: cross,
				}
			case sections >= 4:
				flag := parts[1]
				a, ok := aff.AffixMap[flag]
				if !ok {
					return nil, fmt.Errorf("got rules for flag %q but no definition", flag)
				}

				strip := ""
				if parts[2] != "0" {
					strip = parts[2]
				}

				// The condition is optional; default to "." (matches anything)
				// when a dictionary omits it. See #776.
				cond := "."
				if sections > 4 {
					cond = parts[4]
				}

				var matcher *regexp.Regexp
				var err error
				if cond != "." {
					pat := cond
					if a.Type == Prefix {
						pat = "^" + pat
					} else {
						pat += "$"
					}
					matcher, err = regexp.Compile(pat)
					if err != nil {
						return nil, fmt.Errorf("unable to compile %s", pat)
					}
				}

				// See #499.
				//
				// TODO: Is this safe to do in all cases?
				affixText := parts[3]
				if affixText == "0" {
					affixText = ""
				} else if i := strings.Index(affixText, "/"); i >= 0 {
					// Strip the affix's own continuation flags, e.g. the
					// "/34,22" in `SFX 1 0 t/34,22 e`. Otherwise they'd be
					// appended to the generated word ("stavet/34,22"), so the
					// real form ("stavet") is never recognized. See #1065.
					//
					// NOTE: We don't yet recursively apply continuation classes,
					// so some further-inflected forms remain unrecognized.
					affixText = affixText[:i]
				}

				a.Rules = append(a.Rules, rule{
					Strip:     strip,
					AffixText: affixText,
					Pattern:   cond,
					matcher:   matcher,
				})
				aff.AffixMap[flag] = a
			}
		default:
			// Do nothing.
			//
			// Hunspell ignores lines that don't start with a known directive.
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return &aff, nil
}
