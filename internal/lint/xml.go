package lint

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/system"
)

// XML configuration.
var xsltArgs = []string{
	"--stringparam",
	"use.extensions",
	"0",
	"--stringparam",
	"generate.toc",
	"nop",
}

func (l Linter) lintXML(file *core.File) error {
	var out bytes.Buffer
	var eut bytes.Buffer

	xsltproc := system.Which([]string{"xsltproc", "xsltproc.exe"})
	if xsltproc == "" {
		return core.NewE100("lintXML", errors.New("xsltproc not found"))
	} else if file.Transform == "" {
		return core.NewE100(
			"lintXML",
			errors.New("no XSLT transform provided"))
	}

	args := append([]string{}, xsltArgs...)
	args = append(args, []string{file.Transform, "-"}...)

	cmd := exec.Command(xsltproc, args...)
	cmd.Stdin = strings.NewReader(file.Content)
	cmd.Stdout = &out
	cmd.Stderr = &eut

	if err := cmd.Run(); err != nil {
		return core.NewE100(file.Path, errors.New(eut.String()))
	}

	return l.lintHTMLTokens(file, out.Bytes(), 0)
}
