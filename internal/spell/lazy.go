package spell

import (
	"sort"
	"strings"
	"sync"
)

// dictionaryFlags is one normalized flag vector attached to a root. Keeping
// homographic entries separate prevents flags from unrelated entries from
// forming an invalid cross-product.
type dictionaryFlags struct {
	text string
}

type reverseAffixIndex struct {
	prefixes      map[string][]rule
	suffixes      map[string][]rule
	prefixLengths []int
	suffixLengths []int
}

// newReverseAffixIndex indexes affix rules by their added prefix or suffix so
// candidate roots can be recovered from a target word.
func newReverseAffixIndex(affixes map[string]affix) reverseAffixIndex {
	index := reverseAffixIndex{
		prefixes: make(map[string][]rule),
		suffixes: make(map[string][]rule),
	}
	prefixLengths := make(map[int]struct{})
	suffixLengths := make(map[int]struct{})
	for _, class := range affixes {
		for _, current := range class.Rules {
			if class.Type == Prefix {
				index.prefixes[current.AffixText] = append(index.prefixes[current.AffixText], current)
				prefixLengths[len(current.AffixText)] = struct{}{}
			} else {
				index.suffixes[current.AffixText] = append(index.suffixes[current.AffixText], current)
				suffixLengths[len(current.AffixText)] = struct{}{}
			}
		}
	}
	for length := range prefixLengths {
		index.prefixLengths = append(index.prefixLengths, length)
	}
	for length := range suffixLengths {
		index.suffixLengths = append(index.suffixLengths, length)
	}
	sort.Ints(index.prefixLengths)
	sort.Ints(index.suffixLengths)
	return index
}

// An expandDepth invocation can apply one prefix and one suffix. The initial
// invocation and each permitted continuation depth may therefore contribute
// two affixes to a generated form.
const maxReverseAffixes = 2 * (maxAffixDepth + 1)

// predecessors adds every plausible one-affix predecessor of word to out.
func (i reverseAffixIndex) predecessors(word string, out map[string]struct{}) {
	for _, length := range i.prefixLengths {
		if length > len(word) {
			break
		}
		text := word[:length]
		for _, current := range i.prefixes[text] {
			candidate := word[length:]
			if current.matcher == nil || current.matcher.MatchString(candidate) {
				out[candidate] = struct{}{}
			}
		}
	}

	for _, length := range i.suffixLengths {
		if length > len(word) {
			break
		}
		text := word[len(word)-length:]
		for _, current := range i.suffixes[text] {
			stem := word[:len(word)-length]
			if current.Strip == "" {
				if current.matcher == nil || current.matcher.MatchString(stem) {
					out[stem] = struct{}{}
				}
				continue
			}

			candidate := stem + current.Strip
			if current.matcher == nil || current.matcher.MatchString(candidate) {
				out[candidate] = struct{}{}
			}
			// The existing forward path appends the affix without stripping when
			// the input does not end in Strip. Preserve that behavior here.
			if !strings.HasSuffix(stem, current.Strip) &&
				(current.matcher == nil || current.matcher.MatchString(stem)) {
				out[stem] = struct{}{}
			}
		}
	}
}

const lazySpellCacheSize = 4096

type spellCache struct {
	mu      sync.Mutex
	values  map[string]bool
	keys    []string
	nextKey int
}

// newSpellCache creates a bounded cache for lazy spelling results.
func newSpellCache() *spellCache {
	return &spellCache{
		values: make(map[string]bool, lazySpellCacheSize),
		keys:   make([]string, 0, lazySpellCacheSize),
	}
}

// get returns the cached result for word and whether it was present.
func (c *spellCache) get(word string) (bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	value, found := c.values[word]
	return value, found
}

// set stores value for word, evicting the oldest ring-buffer slot when full.
func (c *spellCache) set(word string, value bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, found := c.values[word]; found {
		c.values[word] = value
		return
	}
	if len(c.keys) < cap(c.keys) {
		c.keys = append(c.keys, word)
	} else {
		delete(c.values, c.keys[c.nextKey])
		c.keys[c.nextKey] = word
		c.nextKey = (c.nextKey + 1) % len(c.keys)
	}
	c.values[word] = value
}
