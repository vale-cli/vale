package core

// AlertLevels holds the possible values for "level" in an external rule.
var AlertLevels = []string{"suggestion", "warning", "error"}

// LevelToInt allows us to easily compare levels in lint.go.
var LevelToInt = map[string]int{
	"suggestion": 0,
	"warning":    1,
	"error":      2,
}

// An Action represents a possible solution to an Alert.
type Action struct {
	Name   string   // the name of the action -- e.g, 'replace'
	Params []string // a slice of parameters for the given action
}

// An Alert represents a potential error in prose.
type Alert struct {
	Action         Action   // a possible solution
	Span           []int    // the [begin, end] location within a line
	Offset         []string `json:"-"` // tokens to ignore before this match
	Check          string   // the name of the check
	Description    string   // why `Message` is meaningful
	Link           string   // reference material
	Message        string   // the output message
	Severity       string   // 'suggestion', 'warning', or 'error'
	Match          string   // the actual matched text
	Line           int      // the source line
	Limit          int      `json:"-"` // the max times to report
	Hide           bool     `json:"-"` // should we hide this alert?
	HasByteOffsets bool     `json:"-"` // Span holds byte offsets into the raw document
}

// FormatAlert ensures that all required fields have data.
func FormatAlert(a *Alert, limit int, level, name string) {
	if a.Severity == "" {
		a.Severity = level
	}
	if a.Check == "" {
		a.Check = name
	}
	a.Limit = limit
	a.Message = WhitespaceToSpace(a.Message)
}

// ByPosition sorts Alerts by line and column.
type ByPosition []Alert

func (a ByPosition) Len() int      { return len(a) }
func (a ByPosition) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByPosition) Less(i, j int) bool {
	ai, aj := a[i], a[j]

	if ai.Line != aj.Line {
		return ai.Line < aj.Line
	}
	return ai.Span[0] < aj.Span[0]
}

// ByName sorts Files by their path.
type ByName []*File

func (a ByName) Len() int      { return len(a) }
func (a ByName) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByName) Less(i, j int) bool {
	ai, aj := a[i], a[j]
	return ai.Path < aj.Path
}
