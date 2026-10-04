package rich

import (
	"encoding/json"
	"testing"
)

func TestDocumentPureMath(t *testing.T) {
	message := Document(`\int_0^\infty e^{-x^2}\,dx`, true)
	if len(message.Blocks) != 1 || message.Blocks[0].Type != "mathematical_expression" {
		t.Fatalf("blocks = %+v", message.Blocks)
	}
	if message.Blocks[0].Expression != `\int_0^\infty e^{-x^2}\,dx` {
		t.Fatalf("expression = %q", message.Blocks[0].Expression)
	}
}

func TestDocumentDollarMath(t *testing.T) {
	message := Document(`$F=ma$`, true)
	if len(message.Blocks) != 1 || message.Blocks[0].Type != "paragraph" {
		t.Fatalf("blocks = %+v", message.Blocks)
	}
}

func TestDocumentDisplayMath(t *testing.T) {
	message := Document("before\n\n$$F=ma$$\n\nafter", true)
	if len(message.Blocks) != 3 {
		t.Fatalf("blocks = %+v", message.Blocks)
	}
	if message.Blocks[0].Type != "paragraph" || message.Blocks[1].Type != "mathematical_expression" || message.Blocks[2].Type != "paragraph" {
		t.Fatalf("block types = %s/%s/%s", message.Blocks[0].Type, message.Blocks[1].Type, message.Blocks[2].Type)
	}
	if message.Blocks[1].Expression != "F=ma" {
		t.Fatalf("expression = %q", message.Blocks[1].Expression)
	}
}

func TestDocumentTextFormatting(t *testing.T) {
	message := Document(`Hello \textbf{world} and \emph{italics}`, true)
	if len(message.Blocks) != 1 {
		t.Fatalf("blocks = %+v", message.Blocks)
	}
	parts, ok := message.Blocks[0].Text.([]any)
	if !ok || len(parts) != 4 {
		t.Fatalf("text = %#v", message.Blocks[0].Text)
	}
	if parts[0] != "Hello " {
		t.Errorf("parts[0] = %#v", parts[0])
	}
	bold, ok := parts[1].(Inline)
	if !ok || bold.Type != "bold" || bold.Text != "world" {
		t.Errorf("parts[1] = %#v", parts[1])
	}
	if parts[2] != " and " {
		t.Errorf("parts[2] = %#v", parts[2])
	}
	italic, ok := parts[3].(Inline)
	if !ok || italic.Type != "italic" || italic.Text != "italics" {
		t.Errorf("parts[3] = %#v", parts[3])
	}
}

func TestDocumentNestedFormatting(t *testing.T) {
	message := Document(`\textbf{bold \emph{and italic}}`, true)
	parts, ok := message.Blocks[0].Text.([]any)
	if !ok || len(parts) != 1 {
		t.Fatalf("text = %#v", message.Blocks[0].Text)
	}
	bold, ok := parts[0].(Inline)
	if !ok || bold.Type != "bold" {
		t.Fatalf("parts[0] = %#v", parts[0])
	}
	inner, ok := bold.Text.([]any)
	if !ok || len(inner) != 2 {
		t.Fatalf("bold text = %#v", bold.Text)
	}
	italic, ok := inner[1].(Inline)
	if !ok || italic.Type != "italic" || italic.Text != "and italic" {
		t.Fatalf("inner = %#v", inner[1])
	}
}

func TestDocumentMixedTextAndMath(t *testing.T) {
	message := Document(`The area is $\pi r^2$ exactly`, true)
	parts, ok := message.Blocks[0].Text.([]any)
	if !ok || len(parts) != 3 {
		t.Fatalf("text = %#v", message.Blocks[0].Text)
	}
	math, ok := parts[1].(Inline)
	if !ok || math.Type != "mathematical_expression" || math.Expression != `\pi r^2` {
		t.Fatalf("math = %#v", parts[1])
	}
}

func TestDocumentSections(t *testing.T) {
	message := Document("\\section{Intro}\n\nSome text", true)
	if len(message.Blocks) != 2 {
		t.Fatalf("blocks = %+v", message.Blocks)
	}
	if message.Blocks[0].Type != "heading" || message.Blocks[0].Size != 2 {
		t.Fatalf("heading = %+v", message.Blocks[0])
	}
	if message.Blocks[0].Text != "Intro" {
		t.Fatalf("heading text = %#v", message.Blocks[0].Text)
	}
}

func TestDocumentList(t *testing.T) {
	message := Document("\\begin{itemize}\n\\item one\n\\item two\n\\end{itemize}", true)
	if len(message.Blocks) != 2 {
		t.Fatalf("blocks = %+v", message.Blocks)
	}
	if message.Blocks[0].Text != "• one" || message.Blocks[1].Text != "• two" {
		t.Fatalf("items = %#v / %#v", message.Blocks[0].Text, message.Blocks[1].Text)
	}
}

func TestDocumentEscapesAndCommands(t *testing.T) {
	message := Document(`100\% \& more \LaTeX{} \url{https://example.com}`, true)
	parts, ok := message.Blocks[0].Text.([]any)
	if !ok {
		t.Fatalf("text = %#v", message.Blocks[0].Text)
	}
	joined := ""
	for _, part := range parts {
		if text, ok := part.(string); ok {
			joined += text
		}
	}
	if joined != "100% & more LaTeX " {
		t.Fatalf("joined = %q", joined)
	}
	url, ok := parts[len(parts)-1].(Inline)
	if !ok || url.Type != "url" || url.URL != "https://example.com" {
		t.Fatalf("url = %#v", parts[len(parts)-1])
	}
}

func TestDocumentHyperrefCommands(t *testing.T) {
	message := Document(`See \hyperlink{details}{the \textbf{details}}.`, true)
	if len(message.Blocks) != 1 || message.Blocks[0].Type != "paragraph" {
		t.Fatalf("blocks = %+v", message.Blocks)
	}
	parts, ok := message.Blocks[0].Text.([]any)
	if !ok || len(parts) != 4 {
		t.Fatalf("text = %#v", message.Blocks[0].Text)
	}
	if parts[0] != "See " || parts[1] != "the " || parts[3] != "." {
		t.Fatalf("parts = %#v", parts)
	}
	bold, ok := parts[2].(Inline)
	if !ok || bold.Type != "bold" || bold.Text != "details" {
		t.Fatalf("bold = %#v", parts[2])
	}

	target := Document(`\hypertarget{details}{Details}`, true)
	if len(target.Blocks) != 1 || target.Blocks[0].Type != "paragraph" || target.Blocks[0].Text != "Details" {
		t.Fatalf("target blocks = %+v", target.Blocks)
	}
}

func TestDocumentFullDocumentBody(t *testing.T) {
	source := "\\documentclass{article}\n\\begin{document}\nHello \\textbf{world}\n\\end{document}"
	message := Document(source, true)
	if len(message.Blocks) != 1 {
		t.Fatalf("blocks = %+v", message.Blocks)
	}
	if message.Blocks[0].Type != "paragraph" {
		t.Fatalf("block = %+v", message.Blocks[0])
	}
}

func TestInlineDocumentLeadingMath(t *testing.T) {
	message := InlineDocument(`$F=ma$`, true)
	if len(message.Blocks) != 1 {
		t.Fatalf("blocks = %+v", message.Blocks)
	}
	parts, ok := message.Blocks[0].Text.([]any)
	if !ok || len(parts) != 2 || parts[0] != " " {
		t.Fatalf("text = %#v", message.Blocks[0].Text)
	}
	math, ok := parts[1].(Inline)
	if !ok || math.Type != "mathematical_expression" || math.Expression != "F=ma" {
		t.Fatalf("math = %#v", parts[1])
	}
}

func TestInlineDocumentDisplayMath(t *testing.T) {
	message := InlineDocument("$$F=ma$$", true)
	if len(message.Blocks) != 1 || message.Blocks[0].Type != "paragraph" {
		t.Fatalf("blocks = %+v", message.Blocks)
	}
	parts, ok := message.Blocks[0].Text.([]any)
	if !ok || len(parts) != 2 || parts[0] != " " {
		t.Fatalf("text = %#v", message.Blocks[0].Text)
	}
}

func TestInlineDocumentLeadingParagraph(t *testing.T) {
	message := InlineDocument(`Hello \textbf{world}`, true)
	parts, ok := message.Blocks[0].Text.([]any)
	if !ok || len(parts) == 0 || parts[0] != " " {
		t.Fatalf("text = %#v", message.Blocks[0].Text)
	}
}

func TestDocumentJSONMarshal(t *testing.T) {
	data, err := json.Marshal(Document(`Hello \textbf{world}`, true))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	expected := `{"blocks":[{"type":"paragraph","text":["Hello ",{"type":"bold","text":"world"}]}]}`
	if string(data) != expected {
		t.Fatalf("unexpected JSON:\n got %s\nwant %s", data, expected)
	}
}

// blockText returns the math expression or the paragraph text of a block.
func blockText(block Block) string {
	if block.Expression != "" {
		return block.Expression
	}
	if text, ok := block.Text.(string); ok {
		return text
	}
	return ""
}

func TestDocumentSymbolCommands(t *testing.T) {
	// A lone symbol renders as text: client math fonts often lack exotic
	// symbols, while the regular text font has them.
	message := Document(`\heartsuit`, true)
	if len(message.Blocks) != 1 || message.Blocks[0].Type != "paragraph" {
		t.Fatalf("blocks = %+v", message.Blocks)
	}
	if blockText(message.Blocks[0]) != "♡" {
		t.Fatalf("text = %q", blockText(message.Blocks[0]))
	}

	// With surrounding letters it stays a math expression.
	message = Document(`I \heartsuit{} you`, true)
	if message.Blocks[0].Type != "mathematical_expression" || blockText(message.Blocks[0]) != "I ♡ you" {
		t.Fatalf("blocks = %+v", message.Blocks)
	}

	// Standard math commands are left to the client engine, which renders
	// them natively (Unicode replacements are not drawn by every font).
	message = Document(`\int_0^\infty e^{-x^2}\,dx`, true)
	if blockText(message.Blocks[0]) != `\int_0^\infty e^{-x^2}\,dx` {
		t.Fatalf("expression = %q", blockText(message.Blocks[0]))
	}

	for source, want := range map[string]string{
		`\alpha + \beta = \gamma`:           `\alpha + \beta = \gamma`,
		`\sum_{i=1}^n i`:                    `\sum_{i=1}^n i`,
		`a \leq b \to c`:                    `a \leq b \to c`,
		`\mathbb{R}^n`:                      `\mathbb{R}^n`,
		`\mathcal{L}(f)`:                    `\mathcal{L}(f)`,
		`\mathfrak{g} \otimes \mathfrak{h}`: `\mathfrak{g} \otimes \mathfrak{h}`,
		`\mathbf{v} \cdot \nabla f`:         `\mathbf{v} \cdot \nabla f`,
		`\heartsuit`:                        "♡",
	} {
		message := Document(source, true)
		if got := blockText(message.Blocks[0]); got != want {
			t.Errorf("source %q: got %q, want %q", source, got, want)
		}
	}
}

func TestDocumentIntegralCommandPreserved(t *testing.T) {
	source := `$$\int_0^\infty e^{-x^{porcodio}}\,dx$$`
	message := Document(source, true)
	if len(message.Blocks) != 1 || message.Blocks[0].Type != "mathematical_expression" {
		t.Fatalf("blocks = %+v", message.Blocks)
	}
	if got := message.Blocks[0].Expression; got != `\int_0^\infty e^{-x^{porcodio}}\,dx` {
		t.Fatalf("expression = %q, integral command must stay for the client engine", got)
	}
}

func TestDocumentSymbolMathSegments(t *testing.T) {
	// A math segment containing only symbols is hoisted to text: the math
	// font does not carry exotic glyphs, the regular font does.
	for _, source := range []string{`$\heartsuit$`, `$$\heartsuit$$`, `\( \heartsuit \)`, `\[ \heartsuit \]`} {
		message := Document(source, true)
		if len(message.Blocks) != 1 || message.Blocks[0].Type != "paragraph" {
			t.Errorf("source %q: blocks = %+v", source, message.Blocks)
			continue
		}
		if got := blockText(message.Blocks[0]); got != "♡" {
			t.Errorf("source %q: text = %q, want ♡", source, got)
		}
	}

	// Symbols mixed with real math stay in a math node.
	message := Document(`$x \heartsuit y$`, true)
	if message.Blocks[0].Type != "paragraph" {
		t.Fatalf("blocks = %+v", message.Blocks)
	}
	parts, ok := message.Blocks[0].Text.([]any)
	if !ok || len(parts) != 1 {
		t.Fatalf("text = %#v", message.Blocks[0].Text)
	}
	math, ok := parts[0].(Inline)
	if !ok || math.Type != "mathematical_expression" || math.Expression != "x ♡ y" {
		t.Fatalf("math = %#v", parts[0])
	}
}

func TestIsMathExpression(t *testing.T) {
	for source, want := range map[string]bool{
		`\heartsuit`:                        true,
		`\int_0^\infty x`:                   true,
		`F = ma`:                            true,
		`$F=ma$`:                            true,
		`\begin{matrix} a & b \end{matrix}`: true,
		`Hello \textbf{x}`:                  false,
		`\hyperlink{x}{go}`:                 false,
		`\begin{itemize} \item a`:           false,
		`\begin{itemize} \item $x$`:         false,
	} {
		if got := IsMathExpression(source); got != want {
			t.Errorf("IsMathExpression(%q) = %v, want %v", source, got, want)
		}
	}
}

func TestContainsMathEnvironment(t *testing.T) {
	for source, want := range map[string]bool{
		`\begin{align} a &= b \end{align}`:  true,
		`\begin{matrix} a & b \end{matrix}`: true,
		`\begin{itemize} a`:                 false,
		`x^2`:                               false,
	} {
		if got := ContainsMathEnvironment(source); got != want {
			t.Errorf("ContainsMathEnvironment(%q) = %v, want %v", source, got, want)
		}
	}
}

func TestDocumentAlphabetFallback(t *testing.T) {
	// A font command with non-alphanumeric content is left to the engine.
	message := Document(`\mathbb{R_1}`, true)
	if message.Blocks[0].Expression != `\mathbb{R_1}` {
		t.Fatalf("expression = %q", message.Blocks[0].Expression)
	}
}

func TestPureMathKeepsPlainText(t *testing.T) {
	message := Document("F = ma", true)
	if len(message.Blocks) != 1 || message.Blocks[0].Type != "mathematical_expression" {
		t.Fatalf("blocks = %+v", message.Blocks)
	}
}

func TestDocumentTextDefault(t *testing.T) {
	// With defaultMath disabled a bare source is text, not math.
	message := Document("F = ma", false)
	if len(message.Blocks) != 1 || message.Blocks[0].Type != "paragraph" {
		t.Fatalf("blocks = %+v", message.Blocks)
	}
	if message.Blocks[0].Text != "F = ma" {
		t.Fatalf("text = %#v", message.Blocks[0].Text)
	}

	// Delimited math still renders as math.
	message = Document("$F = ma$", false)
	parts, ok := message.Blocks[0].Text.([]any)
	if !ok || len(parts) != 1 {
		t.Fatalf("text = %#v", message.Blocks[0].Text)
	}
	if math, ok := parts[0].(Inline); !ok || math.Type != "mathematical_expression" {
		t.Fatalf("parts[0] = %#v", parts[0])
	}

	// Text-mode commands still work.
	message = Document(`Hello \textbf{world}`, false)
	if _, ok := message.Blocks[0].Text.([]any); !ok {
		t.Fatalf("text = %#v", message.Blocks[0].Text)
	}
}

func TestInlineDocumentTextDefault(t *testing.T) {
	message := InlineDocument("F = ma", false)
	if len(message.Blocks) != 1 || message.Blocks[0].Type != "paragraph" {
		t.Fatalf("blocks = %+v", message.Blocks)
	}
	if message.Blocks[0].Text != " F = ma" {
		t.Fatalf("text = %#v", message.Blocks[0].Text)
	}
}

func TestDocumentBareMathCommands(t *testing.T) {
	for _, source := range []string{
		`\frac{1}{2}`,
		`\begin{align} a &= b \\ c &= d \end{align}`,
		`x = \text{hello}`,
	} {
		message := Document(source, true)
		if len(message.Blocks) != 1 || message.Blocks[0].Type != "mathematical_expression" {
			t.Errorf("source %q: blocks = %+v", source, message.Blocks)
		}
	}
}
