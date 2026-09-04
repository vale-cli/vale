package textfsm

import (
	"strings"
	"testing"
)

//nolint:dupword // a template names its states and values twice by design
const commit = `Value Subject (.+)
Value List Body (.*)
Value List Trailer ([A-Z][\w-]+: .+)

Start
  ^${Subject} -> Body

Body
  ^${Trailer}
  ^${Body}
`

func TestCommit(t *testing.T) {
	tmpl, err := Parse(commit)
	if err != nil {
		t.Fatal(err)
	}
	msg := "fix: report the shortfall\n\nThe scope that fell short.\nSecond line.\n\nSigned-off-by: J <j@x>\n"
	recs, err := tmpl.Run(msg)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("records = %d, want 1", len(recs))
	}
	r := recs[0]
	if got := r["Subject"][0]; got.Text != "fix: report the shortfall" || got.Line != 1 || got.Column != 1 {
		t.Errorf("Subject = %+v", got)
	}
	if got := r["Trailer"]; len(got) != 1 || got[0].Line != 6 {
		t.Errorf("Trailer = %+v", got)
	}
	// Body takes every line the trailer rule left, including blanks.
	var body []string
	for _, c := range r["Body"] {
		body = append(body, c.Text)
	}
	if strings.Join(body, "|") != "|The scope that fell short.|Second line.||" {
		t.Errorf("Body = %q", strings.Join(body, "|"))
	}
}

const transcript = `Value Role (user|assistant)
Value List Text (.*)

Start
  ^(?:user|assistant): -> Continue.Record
  ^${Role}: ${Text}
  ^${Text}
`

// TestRecords pins Record and Continue together: a role line closes the
// previous turn, then is read again to open its own.
func TestRecords(t *testing.T) {
	tmpl, err := Parse(transcript)
	if err != nil {
		t.Fatal(err)
	}
	recs, err := tmpl.Run("user: hi\nthere\nassistant: hello\nuser: bye\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("records = %d, want 3: %v", len(recs), recs)
	}
	first, last := recs[0], recs[2]
	if first["Role"][0].Text != "user" || len(first["Text"]) != 2 || first["Text"][1].Text != "there" {
		t.Errorf("first record = %v", first)
	}
	if last["Role"][0].Text != "user" || last["Text"][0].Text != "bye" || last["Text"][0].Column != 7 {
		t.Errorf("last record = %v", last)
	}
}

func TestErrors(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"no values", "Start\n  ^x\n", "at least one Value"},
		{"no start", "Value A (.)\n\nFoo\n  ^${A}\n", "Start state"},
		{"undefined value", "Value A (.)\n\nStart\n  ^${B}\n", "undefined Value"},
		{"undefined state", "Value A (.)\n\nStart\n  ^${A} -> Nowhere\n", "not defined"},
		{"bad option", "Value Sideways A (.)\n\nStart\n  ^${A}\n", "unknown Value option"},
		{"continue with state", "Value A (.)\n\nStart\n  ^${A} -> Continue Foo\n\nFoo\n  ^${A}\n", "cannot change state"}, //nolint:dupword // the state is named, then defined
	}
	for _, c := range cases {
		_, err := Parse(c.src)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
}

func TestErrorAction(t *testing.T) {
	tmpl, err := Parse("Value A (\\d+)\n\nStart\n  ^${A}\n  ^. -> Error \"not a number\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tmpl.Run("1\nx\n"); err == nil || !strings.Contains(err.Error(), "line 2: not a number") {
		t.Errorf("err = %v", err)
	}
}

func TestRequiredAndEOF(t *testing.T) {
	// Required drops a record without the value; an empty EOF state drops
	// the implied record.
	tmpl, err := Parse("Value Required A (a+)\nValue B (b+)\n\nStart\n  ^${A} -> Record\n  ^${B} -> Record\n")
	if err != nil {
		t.Fatal(err)
	}
	recs, _ := tmpl.Run("bb\naa\nbb\n")
	if len(recs) != 1 || recs[0]["A"][0].Text != "aa" {
		t.Errorf("records = %v", recs)
	}

	tmpl, err = Parse("Value A (a+)\n\nStart\n  ^${A}\n\nEOF\n")
	if err != nil {
		t.Fatal(err)
	}
	if recs, _ = tmpl.Run("aa\n"); len(recs) != 0 {
		t.Errorf("records = %v, want none", recs)
	}
}

func TestColumns(t *testing.T) {
	tmpl, err := Parse("Value A (\\S+)\n\nStart\n  ^héllo ${A}\n")
	if err != nil {
		t.Fatal(err)
	}
	recs, _ := tmpl.Run("héllo wörld\n")
	if got := recs[0]["A"][0]; got.Text != "wörld" || got.Column != 7 {
		t.Errorf("capture = %+v, want wörld at column 7", got)
	}
}
