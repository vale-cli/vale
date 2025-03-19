package check

import (
	"fmt"
	"strings"

	"github.com/errata-ai/regexp2"
	"github.com/jdkato/twine/nlp/tag"
	"github.com/mitchellh/mapstructure"

	"github.com/errata-ai/vale/v3/internal/core"
	"github.com/errata-ai/vale/v3/internal/nlp"
)

// NLPToken represents a token of text with NLP-related attributes.
type NLPToken struct {
	Pattern  string
	Tag      string
	Skip     int
	re       *regexp2.Regexp
	Negate   bool
	optional bool
	start    bool
	end      bool
}

// Sequence looks for a user-defined sequence of tokens.
type Sequence struct {
	Definition   `mapstructure:",squash"`
	Tokens       []NLPToken
	Ignorecase   bool
	needsTagging bool
}

// NewSequence creates a new rule from the provided `baseCheck`.
func NewSequence(cfg *core.Config, generic baseCheck, path string) (Sequence, error) {
	rule := Sequence{}

	err := makeTokens(&rule, generic)
	if err != nil {
		return rule, readStructureError(err, path)
	}

	err = decodeRule(generic, &rule)
	if err != nil {
		return rule, readStructureError(err, path)
	}

	err = checkScopes(rule.Scope, path)
	if err != nil {
		return rule, err
	}

	for i, token := range rule.Tokens {
		if !rule.needsTagging && token.Tag != "" {
			rule.needsTagging = true
		}

		if token.Pattern != "" {
			regex := makeRegexp(
				cfg.WordTemplate,
				rule.Ignorecase,
				func() bool { return false },
				func() string { return "" },
				false)
			regex = fmt.Sprintf(regex, token.Pattern)

			re, errc := regexp2.CompileStd(regex)
			if errc != nil {
				return rule, core.NewE201FromPosition(errc.Error(), path, 1)
			}
			rule.Tokens[i].re = re
		}
	}

	rule.Definition.Scope = []string{"sentence"}
	return rule, nil
}

// Fields provides access to the rule definition.
func (s Sequence) Fields() Definition {
	return s.Definition
}

// Pattern is the internal regex pattern used by this rule.
func (s Sequence) Pattern() string {
	return ""
}

func makeTokens(s *Sequence, generic baseCheck) error {
	for _, token := range generic["tokens"].([]interface{}) {
		tok := NLPToken{}
		if err := mapstructure.WeakDecode(token, &tok); err != nil {
			return err
		}

		tok.optional = true
		for i := tok.Skip; i > 0; i-- {
			tok.start = false
			if i == tok.Skip {
				tok.start = true
			}
			s.Tokens = append(s.Tokens, tok)
		}

		if tok.Pattern != "" || tok.Tag != "" {
			tok.optional = false
			tok.end = true
			s.Tokens = append(s.Tokens, tok)
		}
	}

	delete(generic, "tokens")
	return nil
}

func tokensMatch(token NLPToken, word tag.Token) bool {
	failedTag, err := regexp2.MatchString(token.Tag, word.Tag)
	if err != nil {
		// FIXME: return the error instead ...
		panic(err)
	}

	failedTag = failedTag == token.Negate
	failedTok := token.re != nil && token.re.MatchStringStd(word.Text) == token.Negate

	if (token.Pattern == "" && failedTag) ||
		(token.Tag == "" && failedTok) ||
		(token.Tag != "" && token.Pattern != "") && (failedTag || failedTok) {
		return false
	}

	return true
}

func sequenceMatches(idx int, chk Sequence, target NLPToken, words []tag.Token, history []int) ([]string, int) {
	var text []string

	toks := chk.Tokens

	sizeT := len(toks)
	sizeW := len(words)
	index := 0

	for jdx, tok := range words {
		if tokensMatch(target, tok) && !core.IntInSlice(jdx, history) {
			index = jdx
			// We've found our context.
			//
			// The *first* token with a `pattern` becomes the anchor of our
			// search. From there, we must check both its left- and right-hand
			// sides to ensure the sequence matches.
			if idx > 0 {
				// Check the left-end of the sequence:
				//
				// If the anchor is the first token, then there's no left-hand
				// side to check -- hence, `idx > 0`.
				for i := 1; idx-i >= 0; i++ {
					if jdx-i < 0 {
						return []string{}, index
					}
					tok := toks[idx-i]

					word := words[jdx-i]
					text = append([]string{word.Text}, text...)

					// NOTE: We have to perform this conversion because the token slice is made
					// with the right-hand orientation in mind. For example,
					//
					// optional (start), optional, required (end) -> required, optional, optional
					//
					// (from right to left).
					if tok.Skip > 0 {
						tok.optional = (tok.optional || tok.end) && !tok.start
					}

					mat := tokensMatch(tok, word)
					if !mat && !tok.optional {
						return []string{}, index
					} else if mat && tok.optional {
						break
					}
				}
			}
			if idx < sizeT {
				// Check the right-end of the sequence
				//
				// If the anchor is the last token, then there's no right-hand
				// side to check.
				for i := 0; idx+i < sizeT; i++ {
					if jdx+i >= sizeW {
						return []string{}, index
					}
					tok := toks[idx+i]

					word := words[jdx+i]
					text = append(text, word.Text)

					mat := tokensMatch(tok, word)
					if !mat && !tok.optional {
						return []string{}, index
					} else if mat && tok.optional {
						break
					}
				}
			}
			break
		}
	}

	return text, index
}

func stepsToString(steps []string) string {
	var sb strings.Builder

	for i, step := range steps {
		switch {
		case step == "." || step == "," || step == ":" || step == ";" || step == "!" || step == "?" || step == "'" || step == `"` || step == ")":
			// No space before punctuation or closing parenthesis
			sb.WriteString(step)
		case step == "(":
			// No space before or after an opening parenthesis
			if i > 0 && sb.Len() > 0 {
				lastChar := sb.String()[sb.Len()-1]
				if lastChar != ' ' {
					sb.WriteString(" ")
				}
			}
			sb.WriteString(step)
		case strings.HasPrefix(step, "'"):
			// If the step starts with an apostrophe, attach it without space
			sb.WriteString(step)
		default:
			// Otherwise, add space before the word
			if sb.Len() > 0 {
				sb.WriteString(" ")
			}
			sb.WriteString(step)
		}
	}

	return strings.TrimSpace(sb.String())
}

// Run looks for the user-defined sequence of tokens.
func (s Sequence) Run(blk nlp.Block, f *core.File, _ *core.Config) ([]core.Alert, error) {
	var alerts []core.Alert
	var offset []string
	var history []int

	// This is *always* sentence-scoped.
	words := nlp.TextToTokens(blk.Text, &f.NLP)

	txt := blk.Text
	for idx, tok := range s.Tokens {
		if !tok.Negate && tok.Pattern != "" {
			// We're looking for our "anchor" ...
			for _, loc := range tok.re.FindAllStringIndex(txt, -1) {
				// These are all possible violations in `txt`:
				steps, index := sequenceMatches(idx, s, tok, words, history)
				history = append(history, index)

				if len(steps) > 0 {
					seq := stepsToString(steps)
					ssp := strings.Index(txt, seq)

					a := core.Alert{
						Check: s.Name, Severity: s.Level, Link: s.Link,
						Span: []int{ssp, ssp + len(seq)}, Hide: false,
						Match: seq, Action: s.Action}

					a.Message, a.Description = formatMessages(s.Message,
						s.Description, steps...)
					a.Offset = offset

					alerts = append(alerts, a)
					offset = []string{}
				} else {
					converted, err := re2Loc(txt, loc)
					if err != nil {
						return alerts, err
					}
					offset = append(offset, converted)
				}
			}
			break
		}
	}

	return alerts, nil
}
