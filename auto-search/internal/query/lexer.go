package query

// TokenType represents the type of a lexer token.
type TokenType int

const (
	TokenTerm   TokenType = iota // bare word
	TokenPhrase                  // quoted string
	TokenAnd                     // AND operator
	TokenOr                      // OR operator
	TokenNot                     // NOT operator
	TokenEOF                     // end of input
)

// Token is a single lexer token.
type Token struct {
	Type  TokenType
	Value string
}

// Lexer tokenizes a query string.
type Lexer struct {
	input  string
	pos    int
	tokens []Token
}

// NewLexer creates a new Lexer for the given input.
func NewLexer(input string) *Lexer {
	return &Lexer{input: input}
}

// Tokenize scans the full input and returns all tokens.
func (l *Lexer) Tokenize() []Token {
	for {
		tok := l.next()
		l.tokens = append(l.tokens, tok)
		if tok.Type == TokenEOF {
			break
		}
	}
	return l.tokens
}

func (l *Lexer) next() Token {
	l.skipWhitespace()
	if l.pos >= len(l.input) {
		return Token{Type: TokenEOF}
	}

	// Quoted phrase.
	if l.input[l.pos] == '"' {
		return l.readPhrase()
	}

	// Bare word (may be an operator keyword).
	return l.readWord()
}

func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.input) && (l.input[l.pos] == ' ' || l.input[l.pos] == '\t' || l.input[l.pos] == '\n' || l.input[l.pos] == '\r') {
		l.pos++
	}
}

func (l *Lexer) readPhrase() Token {
	l.pos++ // skip opening quote
	start := l.pos
	for l.pos < len(l.input) && l.input[l.pos] != '"' {
		l.pos++
	}
	value := l.input[start:l.pos]
	if l.pos < len(l.input) {
		l.pos++ // skip closing quote
	}
	return Token{Type: TokenPhrase, Value: value}
}

func (l *Lexer) readWord() Token {
	start := l.pos
	for l.pos < len(l.input) && !isWhitespace(l.input[l.pos]) && l.input[l.pos] != '"' {
		l.pos++
	}
	word := l.input[start:l.pos]
	switch word {
	case "AND":
		return Token{Type: TokenAnd, Value: word}
	case "OR":
		return Token{Type: TokenOr, Value: word}
	case "NOT":
		return Token{Type: TokenNot, Value: word}
	default:
		return Token{Type: TokenTerm, Value: word}
	}
}

func isWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}
