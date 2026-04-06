package search

import (
	"strings"
	"testing"
)

func TestNormalizePlainText(t *testing.T) {
	input := "Hello world. This is plain text."
	doc := Normalize(input)

	if !strings.Contains(doc.Body, "Hello world") {
		t.Errorf("Body = %q, want to contain plain text", doc.Body)
	}
	if doc.Headings != "" {
		t.Errorf("Headings = %q, want empty for plain text", doc.Headings)
	}
}

func TestNormalizeEmpty(t *testing.T) {
	doc := Normalize("")
	if doc.Headings != "" {
		t.Errorf("Headings = %q, want empty", doc.Headings)
	}
	if doc.Body != "" {
		t.Errorf("Body = %q, want empty", doc.Body)
	}
}

func TestNormalizeH1GoesToHeadings(t *testing.T) {
	input := "# My Title\n\nSome body text."
	doc := Normalize(input)

	if !strings.Contains(doc.Headings, "My Title") {
		t.Errorf("Headings = %q, want to contain 'My Title'", doc.Headings)
	}
	if strings.Contains(doc.Body, "My Title") {
		t.Errorf("Body = %q, should not contain heading text 'My Title'", doc.Body)
	}
	if !strings.Contains(doc.Body, "Some body text") {
		t.Errorf("Body = %q, want to contain body text", doc.Body)
	}
}

func TestNormalizeH2GoesToHeadings(t *testing.T) {
	input := "## Section Title\n\nSection body."
	doc := Normalize(input)

	if !strings.Contains(doc.Headings, "Section Title") {
		t.Errorf("Headings = %q, want to contain 'Section Title'", doc.Headings)
	}
}

func TestNormalizeH3GoesToHeadings(t *testing.T) {
	input := "### Subsection\n\nSubsection body."
	doc := Normalize(input)

	if !strings.Contains(doc.Headings, "Subsection") {
		t.Errorf("Headings = %q, want to contain 'Subsection'", doc.Headings)
	}
}

func TestNormalizeH4GoesToBody(t *testing.T) {
	// H4+ should go to body, not headings
	input := "#### Deep Heading\n\nSome text."
	doc := Normalize(input)

	if strings.Contains(doc.Headings, "Deep Heading") {
		t.Errorf("Headings = %q, H4 heading should NOT go to headings buffer", doc.Headings)
	}
	if !strings.Contains(doc.Body, "Deep Heading") {
		t.Errorf("Body = %q, H4 heading should appear in body", doc.Body)
	}
}

func TestNormalizeBold(t *testing.T) {
	input := "This is **bold** text."
	doc := Normalize(input)

	if !strings.Contains(doc.Body, "bold") {
		t.Errorf("Body = %q, want 'bold' without markers", doc.Body)
	}
	if strings.Contains(doc.Body, "**") {
		t.Errorf("Body = %q, should not contain '**' markers", doc.Body)
	}
}

func TestNormalizeItalic(t *testing.T) {
	input := "This is *italic* text."
	doc := Normalize(input)

	if !strings.Contains(doc.Body, "italic") {
		t.Errorf("Body = %q, want 'italic' without markers", doc.Body)
	}
	if strings.Contains(doc.Body, "*italic*") {
		t.Errorf("Body = %q, should not contain '*italic*' markers", doc.Body)
	}
}

func TestNormalizeLink(t *testing.T) {
	input := "Click [here](https://example.com) for more."
	doc := Normalize(input)

	if !strings.Contains(doc.Body, "here") {
		t.Errorf("Body = %q, want link text 'here'", doc.Body)
	}
	if strings.Contains(doc.Body, "https://example.com") {
		t.Errorf("Body = %q, should not contain URL", doc.Body)
	}
	if strings.Contains(doc.Body, "[") || strings.Contains(doc.Body, "]") {
		t.Errorf("Body = %q, should not contain link bracket markers", doc.Body)
	}
}

func TestNormalizeImage(t *testing.T) {
	input := "Look at this: ![diagram alt text](images/diagram.png)"
	doc := Normalize(input)

	if !strings.Contains(doc.Body, "diagram alt text") {
		t.Errorf("Body = %q, want image alt text 'diagram alt text'", doc.Body)
	}
	if strings.Contains(doc.Body, "images/diagram.png") {
		t.Errorf("Body = %q, should not contain image URL", doc.Body)
	}
}

func TestNormalizeBlockquote(t *testing.T) {
	input := "> This is a quoted line.\n> Second line."
	doc := Normalize(input)

	if !strings.Contains(doc.Body, "This is a quoted line") {
		t.Errorf("Body = %q, want blockquote content", doc.Body)
	}
	if strings.Contains(doc.Body, ">") {
		t.Errorf("Body = %q, should not contain '>' blockquote markers", doc.Body)
	}
}

func TestNormalizeFencedCodeBlock(t *testing.T) {
	input := "```go\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n```"
	doc := Normalize(input)

	if !strings.Contains(doc.Body, "func main") {
		t.Errorf("Body = %q, want code block content 'func main'", doc.Body)
	}
	if !strings.Contains(doc.Body, "fmt.Println") {
		t.Errorf("Body = %q, want code block content 'fmt.Println'", doc.Body)
	}
	if strings.Contains(doc.Body, "```") {
		t.Errorf("Body = %q, should not contain fence markers", doc.Body)
	}
}

func TestNormalizeTable(t *testing.T) {
	input := "| Name | Value |\n|------|-------|\n| foo  | bar   |"
	doc := Normalize(input)

	if !strings.Contains(doc.Body, "Name") {
		t.Errorf("Body = %q, want table cell text 'Name'", doc.Body)
	}
	if !strings.Contains(doc.Body, "foo") {
		t.Errorf("Body = %q, want table cell text 'foo'", doc.Body)
	}
	if !strings.Contains(doc.Body, "bar") {
		t.Errorf("Body = %q, want table cell text 'bar'", doc.Body)
	}
	// Alignment row (---) should be stripped
	if strings.Contains(doc.Body, "---") {
		t.Errorf("Body = %q, should not contain alignment row '---'", doc.Body)
	}
}

func TestNormalizeWhitespaceCollapsed(t *testing.T) {
	input := "First sentence.\n\n\n\nSecond sentence."
	doc := Normalize(input)

	// Multiple newlines should be collapsed to a single space
	if strings.Contains(doc.Body, "\n\n") {
		t.Errorf("Body = %q, multiple newlines should be collapsed", doc.Body)
	}
}

func TestNormalizeMultipleHeadings(t *testing.T) {
	input := "# Title One\n\n## Section Two\n\n### Subsection Three\n\nBody text."
	doc := Normalize(input)

	if !strings.Contains(doc.Headings, "Title One") {
		t.Errorf("Headings = %q, want 'Title One'", doc.Headings)
	}
	if !strings.Contains(doc.Headings, "Section Two") {
		t.Errorf("Headings = %q, want 'Section Two'", doc.Headings)
	}
	if !strings.Contains(doc.Headings, "Subsection Three") {
		t.Errorf("Headings = %q, want 'Subsection Three'", doc.Headings)
	}
	if !strings.Contains(doc.Body, "Body text") {
		t.Errorf("Body = %q, want body text", doc.Body)
	}
}

func TestNormalizeCombined(t *testing.T) {
	input := `# Getting Started

Welcome to the **documentation**. This guide covers *setup* and configuration.

## Installation

Run the following command:

` + "```bash\nnpm install myapp\n```" + `

See [the docs](https://example.com/docs) for details.

### Advanced Options

![Architecture diagram](images/arch.png)

> Note: Advanced options require admin access.

| Option | Default |
|--------|---------|
| port   | 8080    |
| debug  | false   |
`

	doc := Normalize(input)

	// Headings check
	if !strings.Contains(doc.Headings, "Getting Started") {
		t.Errorf("Headings = %q, want 'Getting Started'", doc.Headings)
	}
	if !strings.Contains(doc.Headings, "Installation") {
		t.Errorf("Headings = %q, want 'Installation'", doc.Headings)
	}
	if !strings.Contains(doc.Headings, "Advanced Options") {
		t.Errorf("Headings = %q, want 'Advanced Options'", doc.Headings)
	}

	// Body check
	if !strings.Contains(doc.Body, "Welcome to the") {
		t.Errorf("Body = %q, want 'Welcome to the'", doc.Body)
	}
	if !strings.Contains(doc.Body, "documentation") {
		t.Errorf("Body = %q, want 'documentation' (from bold)", doc.Body)
	}
	if strings.Contains(doc.Body, "**") {
		t.Errorf("Body = %q, should not contain '**'", doc.Body)
	}
	if !strings.Contains(doc.Body, "setup") {
		t.Errorf("Body = %q, want 'setup' (from italic)", doc.Body)
	}
	if !strings.Contains(doc.Body, "npm install myapp") {
		t.Errorf("Body = %q, want code content 'npm install myapp'", doc.Body)
	}
	if !strings.Contains(doc.Body, "the docs") {
		t.Errorf("Body = %q, want link text 'the docs'", doc.Body)
	}
	if strings.Contains(doc.Body, "https://example.com") {
		t.Errorf("Body = %q, should not contain URL", doc.Body)
	}
	if !strings.Contains(doc.Body, "Architecture diagram") {
		t.Errorf("Body = %q, want image alt text", doc.Body)
	}
	if !strings.Contains(doc.Body, "Note") {
		t.Errorf("Body = %q, want blockquote content", doc.Body)
	}
	if !strings.Contains(doc.Body, "port") {
		t.Errorf("Body = %q, want table cell 'port'", doc.Body)
	}
	if !strings.Contains(doc.Body, "8080") {
		t.Errorf("Body = %q, want table cell '8080'", doc.Body)
	}
}
