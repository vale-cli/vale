package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/template"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/pterm/pterm"
)

var funcs = template.FuncMap{}

func newBorderlessTable(w io.Writer, wrap int) *tablewriter.Table {
	return tablewriter.NewTable(w,
		tablewriter.WithRowAutoWrap(wrap),
		tablewriter.WithRendition(tw.Rendition{
			Borders: tw.BorderNone,
			Symbols: tw.NewSymbols(tw.StyleNone),
			Settings: tw.Settings{
				Lines:      tw.LinesNone,
				Separators: tw.SeparatorsNone,
			},
		}),
	)
}

func init() {
	funcs["red"] = func(s string) string {
		return pterm.Red(s)
	}
	funcs["blue"] = func(s string) string {
		return pterm.Blue(s)
	}
	funcs["yellow"] = func(s string) string {
		return pterm.Yellow(s)
	}
	funcs["underline"] = func(s string) string {
		return pterm.Underscore.Sprint(s)
	}
	funcs["newTable"] = func(wrap bool) *tablewriter.Table {
		wrapMode := tw.WrapNone
		if wrap {
			wrapMode = tw.WrapNormal
		}
		return newBorderlessTable(os.Stdout, wrapMode)
	}
	funcs["addRow"] = func(t *tablewriter.Table, r []string) *tablewriter.Table {
		t.Append(r)
		return t
	}
	funcs["renderTable"] = func(t *tablewriter.Table) *tablewriter.Table {
		fmt.Println()
		t.Render()
		fmt.Println()
		t.Reset()
		return t
	}
	funcs["jsonEscape"] = func(i string) string {
		b, err := json.Marshal(i)
		if err != nil {
			panic(err)
		}
		return string(b[1 : len(b)-1])
	}
}
