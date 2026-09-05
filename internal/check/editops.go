package check

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

var (
	spaceRuns = regexp.MustCompile(`\s+`)
	groupRef  = regexp.MustCompile(`\$(\$|\d+|\{\d+\})`)
	cardinal  = regexp.MustCompile(`^-?\d+$`)
	ordinalRe = regexp.MustCompile(`(?i)^(\d+)(st|nd|rd|th)$`)
)

// expandGroups fills `$N` references in an argument from the match and its
// token's groups: `$0` is the match, `$1` the first group, `$$` a dollar.
func expandGroups(arg, match string, groups []string) string {
	if !strings.Contains(arg, "$") {
		return arg
	}
	return groupRef.ReplaceAllStringFunc(arg, func(ref string) string {
		body := strings.Trim(ref[1:], "{}")
		if body == "$" {
			return "$"
		}
		n, _ := strconv.Atoi(body)
		if n == 0 {
			return match
		}
		if n <= len(groups) {
			return groups[n-1]
		}
		return ""
	})
}

func squeeze(s string) string {
	return spaceRuns.ReplaceAllString(s, " ")
}

func capitalize(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}

func uncapitalize(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if size == 0 {
		return s
	}
	return string(unicode.ToLower(r)) + s[size:]
}

// smartPunctuation turns typewriter punctuation into its typographic form:
// `---` and `--` into dashes, `...` into an ellipsis, and straight quotes into
// curly ones, opening after whitespace or a bracket and closing otherwise.
func smartPunctuation(s string) string {
	s = strings.ReplaceAll(s, "---", "—")
	s = strings.ReplaceAll(s, "--", "–")
	s = strings.ReplaceAll(s, "...", "…")

	var b strings.Builder
	var prev rune
	for i, r := range s {
		switch r {
		case '"':
			if i == 0 || opensQuote(prev) {
				b.WriteRune('“')
			} else {
				b.WriteRune('”')
			}
		case '\'':
			if i == 0 || opensQuote(prev) {
				b.WriteRune('‘')
			} else {
				b.WriteRune('’')
			}
		default:
			b.WriteRune(r)
		}
		prev = r
	}
	return b.String()
}

func opensQuote(prev rune) bool {
	return unicode.IsSpace(prev) || strings.ContainsRune("([{<—–/", prev)
}

var dumbReplacer = strings.NewReplacer(
	"‘", "'", "’", "'",
	"“", "\"", "”", "\"",
	"—", "---", "–", "--",
	"…", "...", " ", " ",
)

// dumbPunctuation is the inverse of smartPunctuation.
func dumbPunctuation(s string) string {
	return dumbReplacer.Replace(s)
}

var asciiLetters = strings.NewReplacer(
	"æ", "ae", "Æ", "AE", "œ", "oe", "Œ", "OE", "ß", "ss",
	"ø", "o", "Ø", "O", "đ", "d", "Đ", "D", "ł", "l", "Ł", "L",
	"þ", "th", "Þ", "Th", "ð", "d", "Ð", "D",
)

// toASCII strips accents, spells out the letters that have no base form,
// and straightens punctuation.
func toASCII(s string) string {
	s = asciiLetters.Replace(dumbPunctuation(s))

	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, s)
	if err != nil {
		return s
	}
	return out
}

var (
	ones = []string{"zero", "one", "two", "three", "four", "five", "six",
		"seven", "eight", "nine", "ten", "eleven", "twelve", "thirteen",
		"fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"}
	tens = []string{"", "", "twenty", "thirty", "forty", "fifty", "sixty",
		"seventy", "eighty", "ninety"}
	scales = []struct {
		value int64
		name  string
	}{
		{1_000_000_000_000, "trillion"},
		{1_000_000_000, "billion"},
		{1_000_000, "million"},
		{1_000, "thousand"},
	}
	irregularOrdinals = map[string]string{
		"one": "first", "two": "second", "three": "third", "five": "fifth",
		"eight": "eighth", "nine": "ninth", "twelve": "twelfth",
	}
)

func cardinalWords(n int64) string {
	switch {
	case n < 0:
		return "minus " + cardinalWords(-n)
	case n < 20:
		return ones[n]
	case n < 100:
		s := tens[n/10]
		if n%10 != 0 {
			s += "-" + ones[n%10]
		}
		return s
	case n < 1000:
		s := ones[n/100] + " hundred"
		if n%100 != 0 {
			s += " " + cardinalWords(n%100)
		}
		return s
	}

	for _, scale := range scales {
		if n >= scale.value {
			s := cardinalWords(n/scale.value) + " " + scale.name
			if rem := n % scale.value; rem != 0 {
				s += " " + cardinalWords(rem)
			}
			return s
		}
	}
	return strconv.FormatInt(n, 10)
}

// ordinalWord turns the last word of a number into its ordinal form.
func ordinalWord(word string) string {
	if irregular, ok := irregularOrdinals[word]; ok {
		return irregular
	}
	if strings.HasSuffix(word, "y") {
		return strings.TrimSuffix(word, "y") + "ieth"
	}
	return word + "th"
}

func ordinalWords(n int64) string {
	s := cardinalWords(n)
	i := strings.LastIndexAny(s, " -")
	return s[:i+1] + ordinalWord(s[i+1:])
}

// numberToWords spells out an integer or an ordinal, `21` or `21st`, and
// leaves anything else alone.
func numberToWords(s string) string {
	plain := strings.ReplaceAll(s, ",", "")

	if cardinal.MatchString(plain) {
		if n, err := strconv.ParseInt(plain, 10, 64); err == nil {
			return cardinalWords(n)
		}
	}
	if m := ordinalRe.FindStringSubmatch(plain); m != nil {
		if n, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			return ordinalWords(n)
		}
	}
	return s
}

// numberWords maps every word numberToWords can produce to its value, with
// ordinal forms marked.
var numberWords = func() map[string]struct {
	value   int64
	ordinal bool
} {
	words := map[string]struct {
		value   int64
		ordinal bool
	}{}
	add := func(word string, value int64) {
		words[word] = struct {
			value   int64
			ordinal bool
		}{value, false}
		words[ordinalWord(word)] = struct {
			value   int64
			ordinal bool
		}{value, true}
	}
	for i, w := range ones {
		add(w, int64(i))
	}
	for i, w := range tens {
		if w != "" {
			add(w, int64(i*10))
		}
	}
	add("hundred", 100)
	for _, scale := range scales {
		add(scale.name, scale.value)
	}
	return words
}()

// wordsToNumber reads a spelled-out integer or ordinal back into digits, and
// leaves anything else alone.
func wordsToNumber(s string) string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return r == ' ' || r == '-'
	})
	if len(fields) == 0 {
		return s
	}

	var total, current int64
	negative, ordinal := false, false
	for i, field := range fields {
		if field == "and" {
			continue
		}
		if field == "minus" && i == 0 {
			negative = true
			continue
		}

		word, ok := numberWords[field]
		if !ok || (ordinal && !word.ordinal) {
			return s
		}
		if word.ordinal {
			if i != len(fields)-1 {
				return s
			}
			ordinal = true
		}

		switch {
		case word.value == 100:
			if current == 0 {
				current = 1
			}
			current *= 100
		case word.value >= 1000:
			if current == 0 {
				current = 1
			}
			total += current * word.value
			current = 0
		default:
			current += word.value
		}
	}

	n := total + current
	if negative {
		n = -n
	}

	out := strconv.FormatInt(n, 10)
	if ordinal {
		out += ordinalSuffix(n)
	}
	return out
}

func ordinalSuffix(n int64) string {
	if n < 0 {
		n = -n
	}
	if n%100 >= 11 && n%100 <= 13 {
		return "th"
	}
	switch n % 10 {
	case 1:
		return "st"
	case 2:
		return "nd"
	case 3:
		return "rd"
	}
	return "th"
}
