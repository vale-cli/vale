package check

import (
	"testing"

	"github.com/jdkato/prose/v3/strcase"
)

type changeCase struct {
	match      bool
	heading    string
	exceptions []string
}

func TestSentence(t *testing.T) {
	headings := []changeCase{
		{heading: "Top-level entities", match: true},
		{heading: "Non-member Predicates", match: false},
		{heading: "Non-member predicates", match: true},
		{heading: "Client's key share and finish", match: true},
		{heading: "Client’s key share and finish", match: true},
		{
			heading: "Find the thief: Introduction",
			match:   true,
		},
		{
			heading:    "Creating a connection to Event Store",
			match:      true,
			exceptions: []string{"Event Store"},
		},
		{
			heading:    "Using errata-ai/vale",
			match:      true,
			exceptions: []string{"errata-ai/vale"},
		},
		{
			heading: "Use the Package Builder to install",
			match:   false,
		},
		{
			heading:    "Use the Package Builder to install",
			match:      true,
			exceptions: []string{"Package Builder"},
		},
		{
			heading: "Use the High-Def Render Pipeline to install",
			match:   false,
		},
		{
			heading: "1. An important heading",
			match:   true,
		},
		{
			heading: "An important heading",
			match:   true,
		},
		{
			heading: "An important Heading",
			match:   false,
		},
		{
			heading: "this isn't in sentence case",
			match:   false,
		},
		{
			heading: "Community Leader responsibilities",
			match:   false,
		},
		{
			heading:    "Community Leader responsibilities",
			exceptions: []string{"Community Leader"},
			match:      true,
		},
		{
			heading: "TAP endpoint",
			match:   true,
		},
		{
			heading: "Configure the CLI",
			match:   true,
		},
		{
			heading: "Querying the TAP Service",
			match:   false,
		},
	}

	for _, h := range headings {
		sc := strcase.NewSentenceConverter(strcase.UsingVocab(h.exceptions))
		_, s := sentence(h.heading, nil, sc, 1)
		if s != h.match {
			t.Errorf("expected = %v, got = %v (%s)", h.match, s, h.heading)
		}
	}
}
