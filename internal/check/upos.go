package check

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Universal POS tags, expressed in terms of the Penn Treebank tags Vale's
// English tagger produces.
//
// Every other language's tagger emits the universal tags themselves, so a
// pattern also accepts its own name: `upos: NOUN` is "NN or NNS from the
// English tagger, or NOUN from any other". See uposTagPattern.
//
// Penn is the finer tagset, so most of this is a straight widening: NOUN is
// "NN or NNS", ADJ is "JJ, JJR or JJS". Three categories are not expressible
// that way, because the distinction Penn omits is syntactic rather than
// morphological:
//
//   - Penn has no AUX at all. "have" is VBP whether it is the main verb of
//     "I have a car" or the auxiliary of "I have eaten".
//   - IN covers both ADP and SCONJ: "after dinner" and "after we ate".
//   - TO is PART in "to eat" but ADP in "to the store".
//
// Those are approximated with a word list alongside the tag; see uposWords.
// The approximation is stated in the docs rather than hidden, because a rule
// author needs to know that `upos: AUX` means "an auxiliary-looking verb", not
// "an auxiliary".
var uposTags = map[string]string{
	"ADJ":   `JJ|JJR|JJS`,
	"ADP":   `IN|RP`,
	"ADV":   `RB|RBR|RBS|WRB`,
	"AUX":   `MD|VB|VBD|VBG|VBN|VBP|VBZ`,
	"CCONJ": `CC`,
	"DET":   `DT|PDT|WDT|WP\$`,
	"INTJ":  `UH`,
	"NOUN":  `NN|NNS`,
	"NUM":   `CD`,
	"PART":  `TO|RP|POS`,
	"PRON":  `PRP|PRP\$|WP|EX`,
	"PROPN": `NNP|NNPS`,
	"PUNCT": `[.,:]|''|` + "``" + `|-LRB-|-RRB-|\(|\)`,
	"SCONJ": `IN|WRB`,
	"SYM":   `SYM|\$|#`,
	"VERB":  `VB|VBD|VBG|VBN|VBP|VBZ`,
	"X":     `FW|LS`,
}

// uposWords narrows the categories Penn cannot distinguish on its own.
//
// A token must carry both the tag and one of these words to match. Without
// this, `upos: AUX` would match every verb in the document. The lists are
// English, and apply only to Penn-tagged words: a universal tagger says AUX
// itself, in its own language.
var uposWords = map[string][]string{
	"AUX": {
		"am", "is", "are", "was", "were", "be", "been", "being",
		"have", "has", "had", "having",
		"do", "does", "did", "doing",
		"will", "would", "shall", "should", "can", "could",
		"may", "might", "must", "ought",
		"'s", "'re", "'m", "'ve", "'ll", "'d",
	},
	"SCONJ": {
		"after", "although", "as", "because", "before", "even", "if",
		"since", "so", "than", "that", "though", "unless", "until",
		"when", "whenever", "where", "whereas", "wherever", "whether",
		"while", "why", "how",
	},
}

// uposTagPattern returns the Penn-tag regex for a universal tag.
//
// Several tags may be given, separated by `|`, for a token that may be any of
// them -- `PRON|PROPN` for something a pronoun or a name could fill.
func uposTagPattern(name string) (string, error) {
	parts := strings.Split(name, "|")
	out := make([]string, 0, len(parts))

	for _, part := range parts {
		part = strings.TrimSpace(part)

		upper := strings.ToUpper(part)
		pattern, ok := uposTags[upper]
		if !ok {
			return "", fmt.Errorf("unknown universal POS tag %q; expected one of %s",
				part, strings.Join(uposNames(), ", "))
		}
		out = append(out, pattern, upper)
	}

	return "^(?:" + strings.Join(out, "|") + ")$", nil
}

// isUniversalTag reports whether tag is one of the universal tags, as a
// non-English tagger emits them.
func isUniversalTag(tag string) bool {
	_, ok := uposTags[tag]
	return ok
}

// uposWordPattern returns the word-level constraint for a universal tag, or
// "" when the tag needs none.
func uposWordPattern(name string) string {
	var words []string

	for _, part := range strings.Split(name, "|") {
		w, ok := uposWords[strings.ToUpper(strings.TrimSpace(part))]
		if !ok {
			// One alternative accepts any word, so the union does too.
			return ""
		}
		words = append(words, w...)
	}
	if len(words) == 0 {
		return ""
	}

	escaped := make([]string, 0, len(words))
	for _, w := range words {
		escaped = append(escaped, regexp.QuoteMeta(w))
	}

	return "^(?:" + strings.Join(escaped, "|") + ")$"
}

// uposNames lists the supported tags, sorted, for error messages.
func uposNames() []string {
	names := make([]string, 0, len(uposTags))
	for name := range uposTags {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
