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

// hunspellConditionPattern converts the part of Hunspell's affix-condition
// syntax that differs from Go regular expressions. A '-' inside a Hunspell
// character group is always literal; it never introduces a range. Encoding it
// as a hexadecimal escape preserves that meaning when regexp compiles it.
func hunspellConditionPattern(condition string, atype affixType) string {
	var pattern strings.Builder
	insideGroup := false

	for _, char := range condition {
		switch char {
		case '[':
			insideGroup = true
		case ']':
			insideGroup = false
		case '-':
			if insideGroup {
				pattern.WriteString(`\x2d`)
				continue
			}
		}
		pattern.WriteRune(char)
	}

	if atype == Prefix {
		return "^" + pattern.String()
	}
	return pattern.String() + "$"
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
	// maxFlagAliasCapacity caps only the initial allocation for an AF table.
	maxFlagAliasCapacity = 1 << 14
)

type dictConfig struct {
	IconvReplacements []string
	Replacements      [][2]string
	CompoundRule      []string
	FlagAliases       []string
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
//   - "ASCII" (default): each byte is a flag
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
	case "UTF-8":
		flags := make([]string, 0, len(flagStr))
		for _, r := range flagStr {
			flags = append(flags, string(r))
		}
		return flags
	default: // "ASCII", Hunspell's default extended 8-bit format.
		flags := make([]string, 0, len(flagStr))
		for i := range len(flagStr) {
			flags = append(flags, flagStr[i:i+1])
		}
		return flags
	}
}

// parseSingleFlag decodes directives that name exactly one flag. In the
// default 8-bit mode this intentionally returns the first byte even when the
// AFF file itself is UTF-8 encoded. Hunspell applies the same byte identifier
// to AFF class names and DIC flag vectors.
func (a dictConfig) parseSingleFlag(flagStr string) (string, error) {
	flags := a.parseFlags(flagStr)
	if len(flags) == 0 || flags[0] == "" {
		return "", fmt.Errorf("empty flag")
	}
	return flags[0], nil
}

// expand expands a word/affix using dictionary/affix rules
//
// This is the dictionary-entry expansion path, so an AF table makes the text
// after the slash a one-based alias index.
//
//	This also supports CompoundRule flags.
func (a dictConfig) expand(wordAffix string, out []string) ([]string, error) {
	word, flags, err := a.dictionaryEntry(wordAffix)
	if err != nil {
		return nil, err
	}
	return a.expandDepth(word, flags, out, 0)
}

// dictionaryEntry separates a dictionary root from its normalized flag
// vector. AF aliases apply only here, at the original .dic entry.
func (a dictConfig) dictionaryEntry(wordAffix string) (string, string, error) {
	idx := strings.Index(wordAffix, "/")
	if idx == -1 {
		return wordAffix, "", nil
	}
	if idx == 0 || idx+1 == len(wordAffix) {
		return "", "", fmt.Errorf("slash char found in first or last position")
	}

	word, flags := wordAffix[:idx], wordAffix[idx+1:]
	return word, a.resolveFlagAlias(flags), nil
}

// resolveFlagAlias expands the one-based AF alias used by both dictionary
// entries and affix continuation classes. Hunspell treats an invalid alias as
// an empty flag vector, leaving the generated word without further affixes.
func (a dictConfig) resolveFlagAlias(flags string) string {
	if len(a.FlagAliases) == 0 {
		return flags
	}

	aliasIndex, err := strconv.ParseInt(flags, 10, 64)
	if err != nil || aliasIndex < 1 || aliasIndex > int64(len(a.FlagAliases)) {
		return ""
	}
	return a.FlagAliases[aliasIndex-1]
}

// expandDepth is expand, tracking how many continuation classes deep it is.
func (a dictConfig) expandDepth(word, keyString string, out []string, depth int) ([]string, error) {
	out = out[:0]
	_, err := a.walkDepth(word, keyString, depth, func(generated string) bool {
		out = append(out, generated)
		return false
	})
	return out, err
}

// expandsTo reports whether an entry can generate target. It shares the exact
// forward traversal used by expand, but stops at the first match and does not
// retain unrelated forms.
func (a dictConfig) expandsTo(word, keyString, target string) bool {
	return a.expandsToWithin(word, keyString, target, nil)
}

// expandsToWithin reports whether an entry can generate target while limiting
// continuation traversal to forms in allowed. A nil set permits every form.
func (a dictConfig) expandsToWithin(
	word, keyString, target string,
	allowed map[string]struct{},
) bool {
	found, _ := a.walkDepthWithin(word, keyString, 0, allowed, func(generated string) bool {
		return generated == target
	})
	return found
}

// hasFlaggedForm reports whether target occurs at an expansion state carrying
// flag. COMPOUNDRULE groups are populated from exactly these states in the
// eager implementation, including continuation-produced forms.
func (a dictConfig) hasFlaggedForm(word, keyString, target, flag string) bool {
	return a.hasFlaggedFormWithin(word, keyString, target, flag, nil)
}

// hasFlaggedFormWithin reports whether target occurs at an expansion state
// carrying flag while limiting continuation traversal to forms in allowed.
func (a dictConfig) hasFlaggedFormWithin(
	word, keyString, target, flag string,
	allowed map[string]struct{},
) bool {
	return a.hasFlaggedFormDepth(word, keyString, target, flag, 0, allowed)
}

// hasFlaggedFormDepth recursively searches affix and continuation states for
// target carrying wanted, stopping after the configured continuation depth.
func (a dictConfig) hasFlaggedFormDepth(
	word, keyString, target, wanted string,
	depth int,
	allowed map[string]struct{},
) bool {
	flags := a.parseFlags(keyString)
	if word == target && containsFlag(flags, wanted) {
		return true
	}

	for _, flag := range flags {
		if flag == a.CompoundOnly {
			return false
		}
	}

	prefixes := make([]affix, 0, 5)
	suffixes := make([]affix, 0, 5)
	for _, flag := range flags {
		class, found := a.AffixMap[flag]
		if !found {
			continue
		}
		if !class.CrossProduct {
			if a.formsHaveFlag(class.forms(word), target, wanted, depth, allowed) {
				return true
			}
			continue
		}
		if class.Type == Prefix {
			prefixes = append(prefixes, class)
		} else {
			suffixes = append(suffixes, class)
		}
	}

	for _, suffix := range suffixes {
		if a.formsHaveFlag(suffix.forms(word), target, wanted, depth, allowed) {
			return true
		}
	}
	for _, prefix := range prefixes {
		prefixForms := prefix.forms(word)
		if a.formsHaveFlag(prefixForms, target, wanted, depth, allowed) {
			return true
		}
		for _, suffix := range suffixes {
			for _, prefixForm := range prefixForms {
				if !wordAllowed(prefixForm.Word, allowed) {
					continue
				}
				if a.formsHaveFlag(suffix.forms(prefixForm.Word), target, wanted, depth, allowed) {
					return true
				}
			}
		}
	}
	return false
}

// formsHaveFlag reports whether one of forms, or one of its continuation
// expansions, produces target with wanted in its continuation flags.
func (a dictConfig) formsHaveFlag(
	forms []form,
	target, wanted string,
	depth int,
	allowed map[string]struct{},
) bool {
	for _, current := range forms {
		continuation := a.resolveFlagAlias(current.Cont)
		if continuation == "" || depth >= maxAffixDepth {
			continue
		}
		if current.Word == target && containsFlag(a.parseFlags(continuation), wanted) {
			return true
		}
		if !wordAllowed(current.Word, allowed) {
			continue
		}
		if a.hasFlaggedFormDepth(current.Word, continuation, target, wanted, depth+1, allowed) {
			return true
		}
	}
	return false
}

// containsFlag reports whether wanted occurs in flags.
func containsFlag(flags []string, wanted string) bool {
	for _, flag := range flags {
		if flag == wanted {
			return true
		}
	}
	return false
}

// walkDepth visits the forms of one dictionary or continuation entry. A true
// visitor result stops the traversal.
func (a dictConfig) walkDepth(
	word, keyString string,
	depth int,
	visit func(string) bool,
) (bool, error) {
	return a.walkDepthWithin(word, keyString, depth, nil, visit)
}

// walkDepthWithin visits generated forms while limiting continuation and
// cross-product traversal to forms in allowed. A nil set permits every form.
func (a dictConfig) walkDepthWithin(
	word, keyString string,
	depth int,
	allowed map[string]struct{},
	visit func(string) bool,
) (bool, error) {
	if keyString == "" {
		return visit(word), nil
	}

	flags := a.parseFlags(keyString)

	// check to see if any of the flags are in the
	// "compound only".  If so then nothing to add
	compoundOnly := false
	for _, key := range flags {
		if key == a.CompoundOnly {
			compoundOnly = true
			continue
		}
	}

	if compoundOnly {
		return false, nil
	}

	if visit(word) {
		return true, nil
	}
	prefixes := make([]affix, 0, 5)
	suffixes := make([]affix, 0, 5)
	for _, key := range flags {
		af, ok := a.AffixMap[key]
		if !ok {
			continue
		}
		if !af.CrossProduct {
			if a.walkForms(af.forms(word), depth, allowed, visit) {
				return true, nil
			}
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
		if a.walkForms(suf.forms(word), depth, allowed, visit) {
			return true, nil
		}
	}
	for _, pre := range prefixes {
		prewords := pre.forms(word)
		if a.walkForms(prewords, depth, allowed, visit) {
			return true, nil
		}

		// now do cross product
		for _, suf := range suffixes {
			for _, w := range prewords {
				if !wordAllowed(w.Word, allowed) {
					continue
				}
				if a.walkForms(suf.forms(w.Word), depth, allowed, visit) {
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// maxAffixDepth bounds how many times a continuation class may be followed.
//
// Hunspell's default is twofold affixation -- one continuation -- and this
// allows one more for dictionaries that lean on longer chains. A bound is what
// makes this safe at all: nothing stops an .aff file from having a class
// continue to itself, and following that faithfully would not terminate.
const maxAffixDepth = 2

// walkForms visits each generated form, then follows any continuation flags it
// carries.
//
// This is the step Hunspell calls twofold affixation: `SFX 1 0 t/34,22 e` says
// that after the rule builds its form, classes 34 and 22 apply to *that*. Not
// following them leaves the further-inflected words unrecognized, which reads
// to a user as their own dictionary not knowing an ordinary word -- most
// visibly in Danish, Dutch and Hungarian, where inflection is built this way.
func (a dictConfig) walkForms(
	forms []form,
	depth int,
	allowed map[string]struct{},
	visit func(string) bool,
) bool {
	for _, f := range forms {
		if visit(f.Word) {
			return true
		}
		continuation := a.resolveFlagAlias(f.Cont)
		if continuation == "" || depth >= maxAffixDepth {
			continue
		}
		if !wordAllowed(f.Word, allowed) {
			continue
		}
		// walkDepth re-emits the word it was given. Filter every equal form as
		// appendForms historically did because it is already visited above.
		found, _ := a.walkDepthWithin(f.Word, continuation, depth+1, allowed, func(word string) bool {
			return word != f.Word && visit(word)
		})
		if found {
			return true
		}
	}
	return false
}

// wordAllowed reports whether word is in allowed, treating a nil set as
// unrestricted.
func wordAllowed(word string, allowed map[string]struct{}) bool {
	if allowed == nil {
		return true
	}
	_, found := allowed[word]
	return found
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
	// A negative value means that no AF table header has been seen. Keep the
	// expected size separately because an alias vector may itself be numeric.
	var flagAliasExpected int64 = -1
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
			if val < 1 || val > maxCompoundMin {
				val = defaultCompoundMin
			}
			// Safe to narrow: the bounds above are well inside an int.
			aff.CompoundMin = int(val)
		case "ONLYINCOMPOUND":
			if len(parts) < 2 {
				return nil, fmt.Errorf("ONLYINCOMPOUND stanza had %d fields, expected 2", len(parts))
			}
			flag, err := aff.parseSingleFlag(parts[1])
			if err != nil {
				return nil, fmt.Errorf("ONLYINCOMPOUND stanza had invalid flag %q", parts[1])
			}
			aff.CompoundOnly = flag
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
			flag, err := aff.parseSingleFlag(parts[1])
			if err != nil {
				return nil, fmt.Errorf("NOSUGGEST stanza had invalid flag %q", parts[1])
			}
			aff.NoSuggestFlag = flag
		case "COMPOUNDFLAG":
			if len(parts) >= 2 {
				flag, err := aff.parseSingleFlag(parts[1])
				if err != nil {
					return nil, fmt.Errorf("COMPOUNDFLAG stanza had invalid flag %q", parts[1])
				}
				aff.CompoundFlag = flag
			}
		case "COMPOUNDBEGIN":
			if len(parts) >= 2 {
				flag, err := aff.parseSingleFlag(parts[1])
				if err != nil {
					return nil, fmt.Errorf("COMPOUNDBEGIN stanza had invalid flag %q", parts[1])
				}
				aff.CompoundBegin = flag
			}
		case "COMPOUNDMIDDLE":
			if len(parts) >= 2 {
				flag, err := aff.parseSingleFlag(parts[1])
				if err != nil {
					return nil, fmt.Errorf("COMPOUNDMIDDLE stanza had invalid flag %q", parts[1])
				}
				aff.CompoundMiddle = flag
			}
		case "COMPOUNDEND":
			if len(parts) >= 2 {
				flag, err := aff.parseSingleFlag(parts[1])
				if err != nil {
					return nil, fmt.Errorf("COMPOUNDEND stanza had invalid flag %q", parts[1])
				}
				aff.CompoundEnd = flag
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
		case "AF":
			if len(parts) < 2 {
				return nil, fmt.Errorf("AF stanza had %d fields, expected at least 2", len(parts))
			}

			if flagAliasExpected < 0 {
				count, err := strconv.ParseInt(parts[1], 10, 64)
				if err != nil || count < 1 {
					return nil, fmt.Errorf("AF stanza had %q, expected positive number", parts[1])
				}
				flagAliasExpected = count
				// The count comes from the file, so cap only the initial
				// allocation. append grows the slice if a larger table is valid.
				aff.FlagAliases = make([]string, 0, int(min(count, int64(maxFlagAliasCapacity))))
				continue
			}

			if int64(len(aff.FlagAliases)) >= flagAliasExpected {
				return nil, fmt.Errorf("AF table had more than %d entries", flagAliasExpected)
			}
			// Keep the vector in its original encoding. It is decoded according
			// to FLAG only when a dictionary entry refers to this alias.
			aff.FlagAliases = append(aff.FlagAliases, parts[1])
		case "PFX", "SFX":
			atype := Prefix
			if parts[0] == "SFX" {
				atype = Suffix
			}
			if len(parts) < 2 {
				return nil, fmt.Errorf("%s stanza had %d fields, expected at least 2", parts[0], len(parts))
			}
			flag, err := aff.parseSingleFlag(parts[1])
			if err != nil {
				return nil, fmt.Errorf("%s stanza had invalid flag %q", parts[0], parts[1])
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
				aff.AffixMap[flag] = affix{
					Type:         atype,
					CrossProduct: cross,
				}
			case sections >= 4:
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
					pat := hunspellConditionPattern(cond, a.Type)
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
	if flagAliasExpected >= 0 && int64(len(aff.FlagAliases)) != flagAliasExpected {
		return nil, fmt.Errorf(
			"AF table had %d entries, expected %d",
			len(aff.FlagAliases), flagAliasExpected)
	}

	return &aff, nil
}
