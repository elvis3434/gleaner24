package query

import (
	"fmt"
	"strings"
	"unicode"
)

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokTerm
	tokAnd
	tokOr
	tokNot
	tokLParen
	tokRParen
)

func (k tokenKind) String() string {
	switch k {
	case tokEOF:
		return "end of expression"
	case tokTerm:
		return "keyword"
	case tokAnd:
		return "AND"
	case tokOr:
		return "OR"
	case tokNot:
		return "NOT"
	case tokLParen:
		return `"("`
	case tokRParen:
		return `")"`
	}
	return "unknown token"
}

type token struct {
	kind tokenKind
	text string // keyword text for tokTerm (quotes removed)
	pos  int    // 1-based column (in runes) where the token starts
}

// SyntaxError reports an invalid expression and the column where it was found.
type SyntaxError struct {
	Pos int // 1-based column, counted in characters
	Msg string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("column %d: %s", e.Pos, e.Msg)
}

// lex splits an expression into tokens. Operators are only recognised in
// upper case so that lower-case "and"/"or"/"not" can still be searched for.
func lex(expr string) ([]token, error) {
	runes := []rune(expr)
	var toks []token
	i := 0
	for i < len(runes) {
		r := runes[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case r == '(':
			toks = append(toks, token{kind: tokLParen, pos: i + 1})
			i++
		case r == ')':
			toks = append(toks, token{kind: tokRParen, pos: i + 1})
			i++
		case r == '"':
			start := i
			i++
			var sb strings.Builder
			closed := false
			for i < len(runes) {
				if runes[i] == '"' {
					closed = true
					i++
					break
				}
				sb.WriteRune(runes[i])
				i++
			}
			if !closed {
				return nil, &SyntaxError{Pos: start + 1, Msg: "unterminated quoted phrase"}
			}
			phrase := strings.TrimSpace(sb.String())
			if phrase == "" {
				return nil, &SyntaxError{Pos: start + 1, Msg: "empty quoted phrase"}
			}
			toks = append(toks, token{kind: tokTerm, text: phrase, pos: start + 1})
		default:
			start := i
			for i < len(runes) && !unicode.IsSpace(runes[i]) && runes[i] != '(' && runes[i] != ')' && runes[i] != '"' {
				i++
			}
			word := string(runes[start:i])
			tok := token{kind: tokTerm, text: word, pos: start + 1}
			switch word {
			case "AND":
				tok.kind = tokAnd
			case "OR":
				tok.kind = tokOr
			case "NOT":
				tok.kind = tokNot
			}
			toks = append(toks, tok)
		}
	}
	toks = append(toks, token{kind: tokEOF, pos: len(runes) + 1})
	return toks, nil
}
