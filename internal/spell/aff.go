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

// form is one generated word and the continuation flags of the rule that
// produced it, which may be empty.
type form struct {
	Word string
	Cont string
}

// forms provides all variations of a given word based on this affix rule,
// each paired with the continuation flags that still apply to it.
func (a affix) forms(word string) []form {
	var out []form
	for _, r := range a.Rules {
		if r.matcher != nil && !r.matcher.MatchString(word) {
			continue
		}
		if a.Type == Prefix {
			out = append(out, form{Word: r.AffixText + word, Cont: r.Cont})
			// TODO is does Strip apply to prefixes too?
		} else {
			stripWord := word
			if r.Strip != "" && strings.HasSuffix(word, r.Strip) {
				stripWord = word[:len(word)-len(r.Strip)]
			}
			out = append(out, form{Word: stripWord + r.AffixText, Cont: r.Cont})
		}
	}
	return out
}

// rule is a Affix rule
type rule struct {
	Strip     string
	AffixText string // suffix or prefix text to add

	// Cont holds the continuation flags the rule carries, if any -- the
	// "34,22" of `SFX 1 0 t/34,22 e`. They name the affix classes that apply
	// again to the form this rule produces, which is how a dictionary spells
	// out an inflection built in more than one step.
	Cont string

	Pattern string         // original matching pattern from AFF file
	matcher *regexp.Regexp // matcher to see if this rule applies or not
}

// dictConfig is a partial representation of a Hunspell AFF (Affix) file.
const (
	// defaultCompoundMin is Hunspell's own default for COMPOUNDMIN.
	defaultCompoundMin = 3
	// maxCompoundMin is where a COMPOUNDMIN stops being a plausible word
	// length and starts being a typo or worse.
	maxCompoundMin = 100
	// maxCompoundRules caps what a COMPOUNDRULE count may preallocate.
	maxCompoundRules = 1 << 16
	// maxBreakRules caps how many BREAK patterns are kept; a real
	// dictionary declares a handful.
	maxBreakRules = 64
)

type dictConfig struct {
	IconvReplacements []string
	Replacements      [][2]string
	CompoundRule      []string
	Break             []string
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
	CompoundMin       int
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
	return a.expandDepth(wordAffix, out, 0)
}

// expandDepth is expand, tracking how many continuation classes deep it is.
func (a dictConfig) expandDepth(wordAffix string, out []string, depth int) ([]string, error) {
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
			out = a.appendForms(af.forms(word), out, depth)
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
		out = a.appendForms(suf.forms(word), out, depth)
	}
	for _, pre := range prefixes {
		prewords := pre.forms(word)
		out = a.appendForms(prewords, out, depth)

		// now do cross product
		for _, suf := range suffixes {
			for _, w := range prewords {
				out = a.appendForms(suf.forms(w.Word), out, depth)
			}
		}
	}
	return out, nil
}

// maxAffixDepth bounds how many times a continuation class may be followed.
//
// Hunspell's default is twofold affixation -- one continuation -- and this
// allows one more for dictionaries that lean on longer chains. A bound is what
// makes this safe at all: nothing stops an .aff file from having a class
// continue to itself, and following that faithfully would not terminate.
const maxAffixDepth = 2

// appendForms adds each generated form to out, then follows any continuation
// flags it carries.
//
// This is the step Hunspell calls twofold affixation: `SFX 1 0 t/34,22 e` says
// that after the rule builds its form, classes 34 and 22 apply to *that*. Not
// following them leaves the further-inflected words unrecognized, which reads
// to a user as their own dictionary not knowing an ordinary word -- most
// visibly in Danish, Dutch and Hungarian, where inflection is built this way.
func (a dictConfig) appendForms(forms []form, out []string, depth int) []string {
	for _, f := range forms {
		out = append(out, f.Word)
		if f.Cont == "" || depth >= maxAffixDepth {
			continue
		}
		// The continuation is expressed exactly like a dictionary entry, so
		// it is expanded as one.
		more, err := a.expandDepth(f.Word+"/"+f.Cont, nil, depth+1)
		if err != nil {
			continue
		}
		// expandDepth re-emits the word it was given; it is already in out.
		for _, w := range more {
			if w != f.Word {
				out = append(out, w)
			}
		}
	}
	return out
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
		CompoundMin: defaultCompoundMin,
	}
	sawBreakCount := false

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
			// Parsed at a fixed width rather than with Atoi, whose `int` is
			// the target's word size: on a 32-bit build that made the value
			// where a number stops being representable -- and so the line
			// between a clamped COMPOUNDMIN and a rejected one -- depend on
			// the architecture. See #1159.
			val, err := strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("COMPOUNDMIN stanza had %q expected number", parts[1])
			}
			// Hunspell ignores a value outside this range and uses its default,
			// rather than refusing the dictionary; a `.aff` is data we are given,
			// so an absurd length is not worth failing over. Bounding it here is
			// also what keeps the value safe to use as a length below.
			aff.CompoundMin = defaultCompoundMin
			if val >= 1 && val <= maxCompoundMin {
				aff.CompoundMin = int(val)
			}
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
				// A count read from the file, so it only preallocates -- the
				// slice grows on its own if the count was low, and a wild one
				// cannot ask for an enormous allocation.
				aff.CompoundRule = make([]string, 0, int(min(max(val, 0), maxCompoundRules)))
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
		case "BREAK":
			if len(parts) < 2 {
				return nil, fmt.Errorf("BREAK stanza had %d fields, expected 2", len(parts))
			}
			// The first BREAK line is a count, which only preallocates; the
			// rest are patterns. See #1165.
			if !sawBreakCount && allDigits(parts[1]) {
				sawBreakCount = true
				continue
			}
			if len(aff.Break) < maxBreakRules {
				aff.Break = append(aff.Break, parts[1])
			}
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
				affixText, cont := parts[3], ""
				if affixText == "0" {
					affixText = ""
				} else if text, flags, found := strings.Cut(affixText, "/"); found {
					// Split off the affix's own continuation flags, e.g. the
					// "/34,22" in `SFX 1 0 t/34,22 e`. Left in place they would
					// be appended to the generated word ("stavet/34,22"), so
					// the real form ("stavet") is never recognized. See #1065.
					//
					// They are kept rather than dropped: the flags name further
					// classes that apply to the form this rule produces, which
					// is how Hunspell builds a word like `stavets` from
					// `stave` in two steps. See expand.
					affixText, cont = text, flags
				}

				a.Rules = append(a.Rules, rule{
					Strip:     strip,
					AffixText: affixText,
					Cont:      cont,
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
