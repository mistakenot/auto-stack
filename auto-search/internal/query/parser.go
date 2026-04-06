package query

import "fmt"

// Parse parses a query string into an AST.
// Adjacent terms/phrases are implicit AND.
// Precedence: NOT > AND > OR.
func Parse(input string) (*Node, error) {
	lexer := NewLexer(input)
	tokens := lexer.Tokenize()
	p := &parser{tokens: tokens}
	node, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.peek().Type != TokenEOF {
		return nil, fmt.Errorf("unexpected token: %q", p.peek().Value)
	}
	return node, nil
}

type parser struct {
	tokens []Token
	pos    int
}

func (p *parser) peek() Token {
	if p.pos >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	return p.tokens[p.pos]
}

func (p *parser) advance() {
	if p.pos < len(p.tokens) {
		p.pos++
	}
}

// parseOr handles OR (lowest precedence).
func (p *parser) parseOr() (*Node, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.peek().Type == TokenOr {
		p.advance() // consume OR
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &Node{Type: NodeOr, Left: left, Right: right}
	}
	return left, nil
}

// parseAnd handles explicit AND and implicit AND (adjacent terms).
func (p *parser) parseAnd() (*Node, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for {
		tok := p.peek()
		if tok.Type == TokenAnd {
			p.advance() // consume AND
			right, err := p.parseNot()
			if err != nil {
				return nil, err
			}
			left = &Node{Type: NodeAnd, Left: left, Right: right}
		} else if tok.Type == TokenTerm || tok.Type == TokenPhrase || tok.Type == TokenNot {
			// Implicit AND: adjacent operands without an explicit operator.
			right, err := p.parseNot()
			if err != nil {
				return nil, err
			}
			left = &Node{Type: NodeAnd, Left: left, Right: right}
		} else {
			break
		}
	}
	return left, nil
}

// parseNot handles NOT (highest precedence among operators).
func (p *parser) parseNot() (*Node, error) {
	if p.peek().Type == TokenNot {
		p.advance() // consume NOT
		operand, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		return &Node{Type: NodeNot, Right: operand}, nil
	}
	return p.parsePrimary()
}

// parsePrimary handles terms and phrases.
func (p *parser) parsePrimary() (*Node, error) {
	tok := p.peek()
	switch tok.Type {
	case TokenTerm:
		p.advance()
		return &Node{Type: NodeTerm, Value: tok.Value}, nil
	case TokenPhrase:
		p.advance()
		return &Node{Type: NodePhrase, Value: tok.Value}, nil
	default:
		return nil, fmt.Errorf("expected term or phrase, got %q", tok.Value)
	}
}
