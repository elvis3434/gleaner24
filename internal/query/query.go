// Package query parses and evaluates boolean keyword expressions such as
//
//	(台積電 OR TSMC) AND (營收 OR 財報) NOT 傳聞
//
// Grammar (NOT binds tighter than AND, AND tighter than OR; two operands
// written side by side are joined with an implicit AND):
//
//	expr    = andExpr { "OR" andExpr }
//	andExpr = unary { ["AND"] unary }
//	unary   = "NOT" unary | primary
//	primary = keyword | '"' phrase '"' | "(" expr ")"
//
// Matching is substring based, so it works for Chinese and Japanese text
// without word segmentation.
package query

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Options controls how keywords and text are compared.
type Options struct {
	// CaseSensitive disables lower-casing before comparison.
	CaseSensitive bool
	// Transform, when set, is applied to both keywords and text after
	// NFKC normalisation (e.g. a Simplified-to-Traditional converter).
	Transform func(string) string
}

// Query is a compiled expression, safe for concurrent use.
type Query struct {
	src  string
	root node
	opts Options
	// terms lists the keywords that count towards a match (those not under
	// NOT), in the order they first appear in the expression.
	terms []*termNode
}

// Parse compiles expr. Syntax errors are returned as *SyntaxError.
func Parse(expr string, opts Options) (*Query, error) {
	// NFKC first so full-width operators and parentheses (（）) work too.
	toks, err := lex(norm.NFKC.String(expr))
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	if p.peek().kind == tokEOF {
		return nil, &SyntaxError{Pos: 1, Msg: "empty expression"}
	}
	root, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if t := p.peek(); t.kind != tokEOF {
		if t.kind == tokRParen {
			return nil, &SyntaxError{Pos: t.pos, Msg: `unmatched ")"`}
		}
		return nil, &SyntaxError{Pos: t.pos, Msg: "unexpected " + t.kind.String()}
	}
	q := &Query{src: expr, root: root, opts: opts}
	seen := map[string]bool{}
	collectTerms(root, false, func(t *termNode) {
		t.norm = q.normalize(t.raw)
		if !seen[t.raw] {
			seen[t.raw] = true
			q.terms = append(q.terms, t)
		}
	})
	// Terms under NOT still need their normalised form for evaluation.
	collectTerms(root, true, func(t *termNode) {
		if t.norm == "" {
			t.norm = q.normalize(t.raw)
		}
	})
	return q, nil
}

// String returns the source expression.
func (q *Query) String() string { return q.src }

// Normalize applies the same normalisation used for keywords: NFKC (so
// full-width and half-width forms compare equal), optional lower-casing and
// the optional Transform.
func (q *Query) Normalize(s string) string {
	return q.normalize(s)
}

func (q *Query) normalize(s string) string {
	s = norm.NFKC.String(s)
	if !q.opts.CaseSensitive {
		s = strings.ToLower(s)
	}
	if q.opts.Transform != nil {
		s = q.opts.Transform(s)
	}
	return s
}

// Match normalises text and reports whether it satisfies the expression.
// When it does, matched lists the positive keywords (as written in the
// expression) that occur in the text.
func (q *Query) Match(text string) (ok bool, matched []string) {
	return q.MatchNormalized(q.normalize(text))
}

// MatchNormalized is like Match for text already passed through Normalize.
func (q *Query) MatchNormalized(text string) (ok bool, matched []string) {
	if !q.root.eval(text) {
		return false, nil
	}
	for _, t := range q.terms {
		if strings.Contains(text, t.norm) {
			matched = append(matched, t.raw)
		}
	}
	return true, matched
}

type node interface {
	eval(text string) bool
}

type termNode struct {
	raw  string
	norm string
}

func (n *termNode) eval(text string) bool { return strings.Contains(text, n.norm) }

type notNode struct{ x node }

func (n *notNode) eval(text string) bool { return !n.x.eval(text) }

type andNode struct{ xs []node }

func (n *andNode) eval(text string) bool {
	for _, x := range n.xs {
		if !x.eval(text) {
			return false
		}
	}
	return true
}

type orNode struct{ xs []node }

func (n *orNode) eval(text string) bool {
	for _, x := range n.xs {
		if x.eval(text) {
			return true
		}
	}
	return false
}

// collectTerms visits term nodes whose polarity (inside an odd number of
// NOTs or not) equals negated.
func collectTerms(n node, negated bool, visit func(*termNode)) {
	var walk func(n node, neg bool)
	walk = func(n node, neg bool) {
		switch v := n.(type) {
		case *termNode:
			if neg == negated {
				visit(v)
			}
		case *notNode:
			walk(v.x, !neg)
		case *andNode:
			for _, x := range v.xs {
				walk(x, neg)
			}
		case *orNode:
			for _, x := range v.xs {
				walk(x, neg)
			}
		}
	}
	walk(n, false)
}

type parser struct {
	toks []token
	i    int
}

func (p *parser) peek() token { return p.toks[p.i] }

func (p *parser) next() token {
	t := p.toks[p.i]
	if t.kind != tokEOF {
		p.i++
	}
	return t
}

func (p *parser) parseOr() (node, error) {
	first, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	xs := []node{first}
	for p.peek().kind == tokOr {
		p.next()
		x, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		xs = append(xs, x)
	}
	if len(xs) == 1 {
		return first, nil
	}
	return &orNode{xs: xs}, nil
}

func startsOperand(k tokenKind) bool {
	return k == tokTerm || k == tokNot || k == tokLParen
}

func (p *parser) parseAnd() (node, error) {
	first, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	xs := []node{first}
	for {
		k := p.peek().kind
		if k == tokAnd {
			p.next()
		} else if !startsOperand(k) {
			break
		}
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		xs = append(xs, x)
	}
	if len(xs) == 1 {
		return first, nil
	}
	return &andNode{xs: xs}, nil
}

func (p *parser) parseUnary() (node, error) {
	if p.peek().kind == tokNot {
		p.next()
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &notNode{x: x}, nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (node, error) {
	t := p.next()
	switch t.kind {
	case tokTerm:
		return &termNode{raw: t.text}, nil
	case tokLParen:
		if p.peek().kind == tokRParen {
			return nil, &SyntaxError{Pos: p.peek().pos, Msg: "empty parentheses"}
		}
		x, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tokRParen {
			return nil, &SyntaxError{Pos: t.pos, Msg: `unclosed "("`}
		}
		p.next()
		return x, nil
	case tokEOF:
		return nil, &SyntaxError{Pos: t.pos, Msg: "expression ends where a keyword was expected"}
	default:
		return nil, &SyntaxError{Pos: t.pos, Msg: "expected a keyword but found " + t.kind.String()}
	}
}
