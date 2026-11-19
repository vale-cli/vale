package regex

import (
	"strings"
	"sync"
)

// scanFloor is the fewest literals worth an automaton: below it, searching
// for each in turn is faster than walking the text a byte at a time.
const scanFloor = 8

// scanCells caps an automaton's table, in cells of four bytes. A set too
// large for it is searched one literal at a time, as before.
const scanCells = 1 << 22

// A scanner finds which of a set of literals a text holds, in one pass over
// it, where a search per literal reads the text once for each. It is an
// Aho-Corasick automaton laid out as a table: one lookup per byte.
type scanner struct {
	class [256]uint16 // a byte's column; 0 is every byte no literal uses
	width int
	next  []int32   // state*width + column -> state
	out   [][]int32 // the literals that end at a state
	empty []int32   // the literals of no length, which every text holds
}

// newScanner builds a scanner for lits, or returns nil when the table would
// be too large to be worth holding.
func newScanner(lits []string) *scanner {
	s := &scanner{width: 1}
	for _, lit := range lits {
		for i := 0; i < len(lit); i++ {
			if s.class[lit[i]] == 0 {
				s.class[lit[i]] = uint16(s.width) //nolint:gosec // at most 256
				s.width++
			}
		}
	}

	size := 1
	for _, lit := range lits {
		size += len(lit)
	}
	if size*s.width > scanCells {
		return nil
	}

	// The trie, with -1 where a state has no child.
	s.next = make([]int32, s.width, size*s.width)
	for i := range s.next {
		s.next[i] = -1
	}
	s.out = make([][]int32, 1, size)

	for id, lit := range lits {
		if lit == "" {
			s.empty = append(s.empty, int32(id))
			continue
		}
		state := 0
		for i := 0; i < len(lit); i++ {
			cell := state*s.width + int(s.class[lit[i]])
			if s.next[cell] < 0 {
				s.next[cell] = int32(len(s.out)) //nolint:gosec // capped above
				for range s.width {
					s.next = append(s.next, -1)
				}
				s.out = append(s.out, nil)
			}
			state = int(s.next[cell])
		}
		s.out[state] = append(s.out[state], int32(id))
	}

	// Breadth first, so a state's fallback is finished before the state is:
	// a missing child becomes the fallback's, and what ends at the fallback
	// ends here too.
	fail := make([]int32, len(s.out))
	queue := make([]int32, 0, len(s.out))
	for c := range s.width {
		if child := s.next[c]; child > 0 {
			queue = append(queue, child)
		} else {
			s.next[c] = 0
		}
	}
	for len(queue) > 0 {
		state := int(queue[0])
		queue = queue[1:]
		back := int(fail[state])
		s.out[state] = append(s.out[state], s.out[back]...)

		for c := range s.width {
			cell := state*s.width + c
			if child := s.next[cell]; child >= 0 {
				fail[child] = s.next[back*s.width+c]
				queue = append(queue, child)
			} else {
				s.next[cell] = s.next[back*s.width+c]
			}
		}
	}

	return s
}

// scan sets the bit of each literal text holds.
func (s *scanner) scan(text string, seen []uint64) {
	for _, id := range s.empty {
		seen[id>>6] |= 1 << (id & 63)
	}

	state := 0
	for i := 0; i < len(text); i++ {
		state = int(s.next[state*s.width+int(s.class[text[i]])])
		for _, id := range s.out[state] {
			seen[id>>6] |= 1 << (id & 63)
		}
	}
}

// any reports whether text holds one of the literals.
func (s *scanner) any(text string) bool {
	if len(s.empty) > 0 {
		return true
	}

	state := 0
	for i := 0; i < len(text); i++ {
		state = int(s.next[state*s.width+int(s.class[text[i]])])
		if len(s.out[state]) > 0 {
			return true
		}
	}
	return false
}

// A FilterSet holds the tests of many patterns and reports which of them a
// text could match. The patterns of one rule share most of their text, so
// their literals are looked for together.
type FilterSet struct {
	tests [][][]string // each pattern's tests, as Requirements gives them

	once  sync.Once
	scan  *scanner
	ids   [][][]int32 // tests, with each literal as its bit
	words int         // the length of the bit set
}

// NewFilterSet returns a set over tests, one entry per pattern. A pattern
// with no tests could match anything.
func NewFilterSet(tests [][][]string) *FilterSet {
	return &FilterSet{tests: tests}
}

// build numbers the literals and, when there are enough, builds the scanner.
// It runs on first use: most patterns compiled are never asked.
func (f *FilterSet) build() {
	index := map[string]int32{}
	var lits []string

	ids := make([][][]int32, len(f.tests))
	for i, tests := range f.tests {
		ids[i] = make([][]int32, len(tests))
		for j, clause := range tests {
			ids[i][j] = make([]int32, len(clause))
			for k, lit := range clause {
				id, ok := index[lit]
				if !ok {
					id = int32(len(lits)) //nolint:gosec // an index
					index[lit] = id
					lits = append(lits, lit)
				}
				ids[i][j][k] = id
			}
		}
	}

	if len(lits) < scanFloor {
		return
	}
	if f.scan = newScanner(lits); f.scan != nil {
		f.ids = ids
		f.words = (len(lits) + 63) / 64
	}
}

// Active lists the patterns lowered could match, by index. A pattern left
// out cannot match.
func (f *FilterSet) Active(lowered string) []int {
	f.once.Do(f.build)

	var out []int
	if f.scan == nil {
		for i, tests := range f.tests {
			if Holds(tests, lowered) {
				out = append(out, i)
			}
		}
		return out
	}

	var buf [16]uint64
	seen := buf[:]
	if f.words > len(buf) {
		seen = make([]uint64, f.words)
	}
	f.scan.scan(lowered, seen)

	for i, tests := range f.ids {
		if holdsSeen(tests, seen) {
			out = append(out, i)
		}
	}
	return out
}

// Any reports whether lowered could match one of the patterns.
func (f *FilterSet) Any(lowered string) bool {
	f.once.Do(f.build)

	if f.scan == nil {
		for _, tests := range f.tests {
			if Holds(tests, lowered) {
				return true
			}
		}
		return false
	}

	// One pattern needing one of its literals is the common shape, and the
	// first literal found settles it.
	if len(f.ids) == 1 && len(f.ids[0]) == 1 {
		return f.scan.any(lowered)
	}

	var buf [16]uint64
	seen := buf[:]
	if f.words > len(buf) {
		seen = make([]uint64, f.words)
	}
	f.scan.scan(lowered, seen)

	for _, tests := range f.ids {
		if holdsSeen(tests, seen) {
			return true
		}
	}
	return false
}

// holdsSeen is Holds over the literals a scan found.
func holdsSeen(tests [][]int32, seen []uint64) bool {
	for _, clause := range tests {
		found := false
		for _, id := range clause {
			if seen[id>>6]&(1<<(id&63)) != 0 {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// Holds reports whether lowered passes every test in tests. A false return
// is definitive: a pattern with those requirements cannot match.
func Holds(tests [][]string, lowered string) bool {
	for _, clause := range tests {
		found := false
		for _, lit := range clause {
			if strings.Contains(lowered, lit) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
