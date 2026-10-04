package rich

import (
	"strings"
	"unicode"
)

// textCommands marks a document as text-mode LaTeX (as opposed to a bare
// math expression) when any of these is present.
var textCommands = map[string]bool{
	"textbf": true, "textit": true, "emph": true, "underline": true,
	"uline": true, "texttt": true, "textsuperscript": true,
	"textsubscript": true, "textrm": true, "textsf": true, "textnormal": true,
	"textup": true, "textmd": true, "textsl": true, "textsc": true,
	"textcolor": true, "color": true, "colorbox": true, "fcolorbox": true,
	"href": true, "url": true, "hyperlink": true, "hypertarget": true,
	"nolinkurl": true, "label": true, "ref": true, "pageref": true,
	"autoref": true, "part": true, "chapter": true, "section": true,
	"subsection": true, "subsubsection": true, "paragraph": true,
	"subparagraph": true, "item": true, "maketitle": true, "title": true,
	"author": true, "date": true, "thanks": true, "documentclass": true,
	"usepackage": true, "includegraphics": true, "caption": true,
	"footnote": true, "marginpar": true, "par": true, "newline": true,
	"linebreak": true, "pagebreak": true, "newpage": true,
	"clearpage": true, "cleardoublepage": true, "noindent": true,
	"indent": true, "centering": true, "raggedright": true,
	"raggedleft": true, "hspace": true, "vspace": true, "hfill": true,
	"vfill": true, "smallskip": true, "medskip": true, "bigskip": true,
	"mbox": true, "makebox": true, "parbox": true, "fbox": true,
	"raisebox": true, "rotatebox": true, "resizebox": true,
	"bfseries": true, "mdseries": true, "rmfamily": true,
	"sffamily": true, "ttfamily": true, "upshape": true, "itshape": true,
	"slshape": true, "scshape": true, "normalfont": true,
	"LaTeX": true, "TeX": true, "copyright": true, "registered": true,
	"circledR": true, "pounds": true, "euro": true, "yen": true,
	"degree": true, "celsius": true, "textbullet": true, "S": true, "P": true,
	"tiny": true, "scriptsize": true, "footnotesize": true,
	"small": true, "normalsize": true, "large": true, "Large": true,
	"LARGE": true, "huge": true, "Huge": true,
}

// mathCommands is deliberately a positive allowlist. An unknown command may
// be a text macro from a user's preamble, so it must never cause automatic
// math wrapping. Users can remove that ambiguity with explicit delimiters.
var mathCommands = commandSet(`
	frac dfrac tfrac binom dbinom tbinom sqrt root
	overline underline overbrace underbrace overset underset stackrel
	hat widehat check widecheck breve acute grave tilde widetilde bar vec
	dot ddot dddot ddddot mathring
	left right middle big Big bigg Bigg bigl bigr Bigl Bigr biggl biggr Biggl Biggr
	mathrm mathbf mathsf mathtt mathit mathnormal mathcal mathbb mathfrak
	boldsymbol pmb operatorname text mod bmod pmod pod
	lim liminf limsup max min sup inf det gcd Pr log ln exp sin cos tan cot sec csc
	arcsin arccos arctan sinh cosh tanh coth ker dim hom arg deg
	alpha beta gamma delta epsilon varepsilon zeta eta theta vartheta iota kappa
	varkappa lambda mu nu xi omicron pi varpi rho varrho sigma varsigma tau
	upsilon phi varphi chi psi omega Gamma Delta Theta Lambda Xi Pi Sigma Upsilon Phi Psi Omega
	aleph beth gimel daleth hbar hslash imath jmath ell wp Re Im partial nabla infty
	forall exists nexists emptyset varnothing neg lnot top bot angle surd prime
	pm mp times div cdot ast star circ bullet cap cup uplus sqcap sqcup vee wedge
	setminus smallsetminus wr diamond bigtriangleup bigtriangledown triangleleft triangleright
	oplus ominus otimes oslash odot bigcirc dagger dag ddagger ddag amalg
	boxplus boxminus boxtimes boxdot ltimes rtimes
	leq le geq ge neq ne equiv models prec succ sim perp preceq succeq simeq mid
	ll gg asymp parallel subset supset approx bowtie subseteq supseteq cong
	in ni owns propto vdash dashv notin nleq ngeq nless ngtr nsim ncong
	leftarrow gets rightarrow to leftrightarrow uparrow downarrow updownarrow
	Leftarrow Rightarrow Leftrightarrow Uparrow Downarrow Updownarrow mapsto longmapsto
	longleftarrow longrightarrow longleftrightarrow Longleftarrow Longrightarrow Longleftrightarrow
	nearrow searrow swarrow nwarrow hookleftarrow hookrightarrow
	sum prod coprod bigcup bigcap bigvee bigwedge bigoplus bigotimes bigodot biguplus bigsqcup
	int iint iiint iiiint oint oiint oiiint smallint
	lceil rceil lfloor rfloor lbrace rbrace langle rangle lvert rvert lVert rVert vert Vert
	dots ldots cdots vdots ddots iddots mathellipsis
`)

func commandSet(names string) map[string]bool {
	set := make(map[string]bool)
	for _, name := range strings.Fields(names) {
		set[name] = true
	}
	return set
}

// sizeCommands contains LaTeX's declaration-style font-size commands. Rich
// messages don't have arbitrary inline font sizes; their only larger text
// primitive is a section heading. Map the five enlarged sizes monotonically
// to heading levels and treat normal/reduced sizes as ordinary paragraphs.
var sizeCommands = map[string]int{
	"tiny": 0, "scriptsize": 0, "footnotesize": 0,
	"small": 0, "normalsize": 0,
	"large": 5, "Large": 4, "LARGE": 3, "huge": 2, "Huge": 1,
}

// wrapperEnvironments are environments whose markers are dropped from text
// mode; their content is rendered inline.
var wrapperEnvironments = map[string]bool{
	"itemize": true, "enumerate": true, "center": true, "flushleft": true,
	"flushright": true, "quote": true, "quotation": true, "description": true,
	"small": true, "footnotesize": true, "large": true, "Large": true,
	"document": true,
}

// documentClassEnvironments are math environments that keep a \begin{...}
// source in math mode.
var mathEnvironments = map[string]bool{
	"equation": true, "equation*": true, "align": true, "align*": true,
	"aligned": true, "gather": true, "gather*": true, "multline": true,
	"multline*": true, "eqnarray": true, "eqnarray*": true, "array": true,
	"matrix": true, "pmatrix": true, "bmatrix": true, "vmatrix": true,
	"Vmatrix": true, "Bmatrix": true, "cases": true, "split": true,
	"displaymath": true, "math": true,
}

// symbolCommands maps exotic symbol commands that client math engines
// commonly lack to Unicode. Standard LaTeX/AMS math commands (\int, \sum,
// Greek letters, arrows, relations, ...) are deliberately NOT translated:
// the client engine renders them natively, while Unicode replacements are
// not drawn by every math font.
var symbolCommands = map[string]string{
	"heartsuit": "♡", "diamondsuit": "♢", "spadesuit": "♠", "clubsuit": "♣",
	"checkmark": "✓", "textbullet": "•", "degree": "°", "celsius": "°C",
	"copyright": "©", "registered": "®", "circledR": "®", "pounds": "£",
	"euro": "€", "yen": "¥", "male": "♂", "female": "♀",
	"flat": "♭", "natural": "♮", "sharp": "♯",
	"Box": "□", "square": "□", "blacksquare": "■",
	"triangle": "△", "blacktriangle": "▲", "blacktriangledown": "▼",
	"bigstar": "★", "lozenge": "◊", "blacklozenge": "⧫",
	"measuredangle": "∡", "sphericalangle": "∢",
	"therefore": "∴", "because": "∵", "maltese": "✠",
	"diagup": "╱", "diagdown": "╲", "complement": "∁", "eth": "ð",
	"Finv": "Ⅎ", "Game": "⅁", "circledS": "Ⓢ", "mho": "℧",
}

// translateSymbols replaces known symbol commands with Unicode. Font
// commands such as \mathbb are left untouched: client math engines render
// them themselves and their Unicode math-alphanumeric fallbacks are not
// supported everywhere.
func translateSymbols(source string) string {
	if !strings.Contains(source, `\`) {
		return source
	}
	var out strings.Builder
	for index := 0; index < len(source); {
		if source[index] != '\\' {
			out.WriteByte(source[index])
			index++
			continue
		}
		name, next := readCommand(source, index)
		if name == "" {
			out.WriteString(source[index:next])
			index = next
			continue
		}
		if replacement, ok := symbolCommands[name]; ok {
			out.WriteString(replacement)
			index = skipEmptyGroup(source, next)
			continue
		}
		out.WriteString(source[index:next])
		index = next
	}
	return out.String()
}

// Document converts a LaTeX source into a rich message. Math segments
// ($...$, $$...$$, \(...\), \[...\]) become math nodes; text paragraphs keep
// their line structure and map the common text-mode formatting commands to
// rich text nodes. With defaultMath, a source with no math delimiters and
// no known text-mode command is treated as a single math expression;
// otherwise such a source is parsed as text and formulas need delimiters.
func Document(source string, defaultMath bool) *Message {
	source = documentBody(source)
	source = translateSymbols(source)
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return &Message{Blocks: []Block{{Type: "paragraph", Text: " "}}}
	}
	if defaultMath && isPureMath(trimmed) {
		if isSymbolsOnly(trimmed) {
			// Client math fonts often lack exotic symbols; the regular
			// text font renders them everywhere.
			return &Message{Blocks: []Block{{Type: "paragraph", Text: trimmed}}}
		}
		return &Message{Blocks: []Block{{Type: "mathematical_expression", Expression: trimmed}}}
	}
	parser := &parser{src: source}
	blocks := parser.parseBlocks()
	if len(blocks) == 0 {
		blocks = []Block{{Type: "paragraph", Text: " "}}
	}
	return &Message{Blocks: blocks}
}

// IsMathExpression reports whether the source is a math expression: bare
// math commands with no text-mode command, a delimited math segment, or a
// math environment. Used to decide whether a failed compile may be retried
// with the expression forced into math mode.
func IsMathExpression(source string) bool {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return false
	}
	if stripOuterMathDelimiters(trimmed) != trimmed {
		return true
	}
	// Delimiters somewhere inside mean mixed text and math: not entirely
	// math, so no retry.
	if strings.Contains(trimmed, "$") || strings.Contains(trimmed, `\(`) || strings.Contains(trimmed, `\[`) {
		return false
	}
	environments := environmentsIn(trimmed)
	for _, environment := range environments {
		if !mathEnvironments[environment] {
			return false
		}
	}
	if len(environments) > 0 {
		return true
	}
	return isPureMath(trimmed)
}

// ContainsMathEnvironment reports whether the source contains a math
// environment such as align or matrix; those compile standalone and must
// not be wrapped again.
func ContainsMathEnvironment(source string) bool {
	for _, environment := range environmentsIn(source) {
		if mathEnvironments[environment] {
			return true
		}
	}
	return false
}

// stripOuterMathDelimiters removes matching outer math delimiters.
func stripOuterMathDelimiters(expr string) string {
	for _, pair := range [][2]string{{`$$`, `$$`}, {`$`, `$`}, {`\[`, `\]`}, {`\(`, `\)`}} {
		if len(expr) >= len(pair[0])+len(pair[1]) &&
			strings.HasPrefix(expr, pair[0]) && strings.HasSuffix(expr, pair[1]) {
			return strings.TrimSpace(expr[len(pair[0]) : len(expr)-len(pair[1])])
		}
	}
	return expr
}

// isSymbolsOnly reports whether the source consists only of symbols and
// punctuation: no letters, digits or LaTeX math structure.
func isSymbolsOnly(source string) bool {
	for _, r := range source {
		switch r {
		case '\\', '_', '{', '}', '$', '&', '#', '^':
			return false
		}
		if r < 128 && !unicode.IsPunct(r) && !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// InlineDocument is Document prepared for inline answers: a leading math
// expression renders inline one space after the bot mention instead of as a
// display block, and a leading paragraph starts with one space.
func InlineDocument(source string, defaultMath bool) *Message {
	message := Document(source, defaultMath)
	if len(message.Blocks) == 0 {
		return message
	}
	first := &message.Blocks[0]
	switch first.Type {
	case "mathematical_expression":
		*first = Block{Type: "paragraph", Text: []any{
			" ",
			Inline{Type: "mathematical_expression", Expression: first.Expression},
		}}
	case "paragraph":
		first.Text = prependSpace(first.Text)
	}
	return message
}

func prependSpace(text any) any {
	switch value := text.(type) {
	case string:
		return " " + value
	case []any:
		return append([]any{" "}, value...)
	default:
		return text
	}
}

// documentBody reduces a full document to the content of its document
// environment.
func documentBody(source string) string {
	const begin = `\begin{document}`
	const end = `\end{document}`
	index := strings.Index(source, begin)
	if index < 0 {
		return source
	}
	source = source[index+len(begin):]
	if index = strings.Index(source, end); index >= 0 {
		source = source[:index]
	}
	return source
}

// isPureMath reports whether the source should be rendered as one math
// expression: no math delimiters, no known text-mode command and no
// non-math environment.
func isPureMath(source string) bool {
	if strings.Contains(source, "$") || strings.Contains(source, `\(`) || strings.Contains(source, `\[`) {
		return false
	}
	environments := environmentsIn(source)
	for _, env := range environments {
		if !mathEnvironments[env] {
			return false
		}
	}
	if len(environments) > 0 {
		return true
	}
	commands := commandsIn(source)
	hasMathCommand := false
	for _, command := range commands {
		if textCommands[command] {
			return false
		}
		if isMathSpacingCommand(command) {
			continue
		}
		if !mathCommands[command] {
			if _, ok := symbolCommands[command]; ok {
				hasMathCommand = true
				continue
			}
			return false
		}
		hasMathCommand = true
	}
	if hasMathCommand {
		return true
	}
	return hasMathSyntax(source)
}

func isMathSpacingCommand(command string) bool {
	switch command {
	case ",", ";", ":", "!", " ":
		return true
	default:
		return false
	}
}

// hasMathSyntax recognizes strong math-only signals in command-free input.
// Plain words and punctuation stay text; ambiguous input can always opt into
// math with explicit delimiters.
func hasMathSyntax(source string) bool {
	if strings.Contains(source, "://") {
		return false
	}
	if strings.ContainsAny(source, "^_=<>") {
		return true
	}
	trimmed := strings.TrimSpace(source)
	if len(trimmed) > 1 && (trimmed[0] == '+' || trimmed[0] == '-') && isASCIIDigit(trimmed[1]) {
		return true
	}
	for index := 1; index+1 < len(source); index++ {
		switch source[index] {
		case '+', '*', '/':
			left, right, ok := surroundingNonSpace(source, index)
			if ok && isMathOperand(left) && isMathOperand(right) &&
				(isASCIIDigit(left) || isASCIIDigit(right) || isSingleLetterOperation(source, index)) {
				return true
			}
		case '-':
			left, right, ok := surroundingNonSpace(source, index)
			if ok && (isASCIIDigit(left) || isASCIIDigit(right) || isSingleLetterOperation(source, index)) {
				return true
			}
		}
	}
	return false
}

func surroundingNonSpace(source string, index int) (byte, byte, bool) {
	left := index - 1
	for left >= 0 && unicode.IsSpace(rune(source[left])) {
		left--
	}
	right := index + 1
	for right < len(source) && unicode.IsSpace(rune(source[right])) {
		right++
	}
	if left < 0 || right >= len(source) {
		return 0, 0, false
	}
	return source[left], source[right], true
}

func isSingleLetterOperation(source string, index int) bool {
	left := index - 1
	for left >= 0 && unicode.IsSpace(rune(source[left])) {
		left--
	}
	right := index + 1
	for right < len(source) && unicode.IsSpace(rune(source[right])) {
		right++
	}
	if left < 0 || right >= len(source) || !isASCIILetter(source[left]) || !isASCIILetter(source[right]) {
		return false
	}
	before := left - 1
	for before >= 0 && unicode.IsSpace(rune(source[before])) {
		before--
	}
	after := right + 1
	for after < len(source) && unicode.IsSpace(rune(source[after])) {
		after++
	}
	return (before < 0 || isMathBoundary(source[before])) &&
		(after >= len(source) || isMathBoundary(source[after]))
}

func isASCIILetter(char byte) bool {
	return (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z')
}

func isMathBoundary(char byte) bool {
	return strings.ContainsRune("+-*/=<>^_,()[]{}", rune(char))
}

func isMathOperand(char byte) bool {
	return isASCIIDigit(char) || (char >= 'A' && char <= 'Z') || (char >= 'a' && char <= 'z') || char == ')' || char == '}'
}

func isASCIIDigit(char byte) bool { return char >= '0' && char <= '9' }

func commandsIn(source string) []string {
	var commands []string
	for i := 0; i < len(source); i++ {
		if source[i] != '\\' || i+1 >= len(source) {
			continue
		}
		if source[i+1] == '\\' {
			commands = append(commands, `\\`)
			i++
			continue
		}
		name := ""
		next := i + 1
		for next < len(source) && isCommandLetter(source[next]) {
			name += string(source[next])
			next++
		}
		if name != "" {
			commands = append(commands, name)
			i = next - 1
		}
	}
	return commands
}

func environmentsIn(source string) []string {
	var envs []string
	for index := 0; index < len(source); {
		position := strings.Index(source[index:], `\begin{`)
		if position < 0 {
			break
		}
		position += index
		start := position + len(`\begin{`)
		end := strings.IndexByte(source[start:], '}')
		if end < 0 {
			break
		}
		envs = append(envs, source[start:start+end])
		index = start
	}
	return envs
}

// readEnvironment reads \begin{name} or \end{name} at index and returns the
// environment name and the position after the closing brace.
func readEnvironment(source string, index int) (string, int, bool) {
	prefix := `\begin{`
	if strings.HasPrefix(source[index:], `\end{`) {
		prefix = `\end{`
	} else if !strings.HasPrefix(source[index:], prefix) {
		return "", index, false
	}
	start := index + len(prefix)
	end := strings.IndexByte(source[start:], '}')
	if end < 0 {
		return "", index, false
	}
	return source[start : start+end], start + end + 1, true
}

func isCommandLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

type parser struct {
	src string
	pos int
}

func (p *parser) eof() bool { return p.pos >= len(p.src) }

func (p *parser) startsWith(prefix string) bool {
	return strings.HasPrefix(p.src[p.pos:], prefix)
}

func (p *parser) parseBlocks() []Block {
	var blocks []Block
	var text strings.Builder
	flush := func() {
		trimmed := strings.TrimSpace(text.String())
		text.Reset()
		if trimmed == "" {
			return
		}
		if content, size, ok := leadingSizeDeclaration(trimmed); ok {
			if content == "" {
				return
			}
			if size > 0 {
				blocks = append(blocks, Block{Type: "heading", Text: parseInline(content), Size: size})
				return
			}
			trimmed = content
		}
		blocks = append(blocks, Block{Type: "paragraph", Text: parseInline(trimmed)})
	}

	for !p.eof() {
		switch {
		case p.startsWith(`\[`):
			flush()
			p.pos += 2
			expression := strings.TrimSpace(p.readUntil(`\]`))
			if isSymbolsOnly(expression) {
				blocks = append(blocks, Block{Type: "paragraph", Text: strings.TrimSpace(expression)})
			} else {
				blocks = append(blocks, Block{Type: "mathematical_expression", Expression: expression})
			}
		case p.startsWith("$$"):
			flush()
			p.pos += 2
			expression := strings.TrimSpace(p.readUntil("$$"))
			if isSymbolsOnly(expression) {
				blocks = append(blocks, Block{Type: "paragraph", Text: strings.TrimSpace(expression)})
			} else {
				blocks = append(blocks, Block{Type: "mathematical_expression", Expression: expression})
			}
		case p.startsWith(`\section`):
			flush()
			p.pos += len(`\section`)
			if title, ok := p.readOptionalStarAndGroup(); ok {
				blocks = append(blocks, Block{Type: "heading", Text: parseInline(title), Size: 2})
			}
		case p.startsWith(`\subsection`):
			flush()
			p.pos += len(`\subsection`)
			if title, ok := p.readOptionalStarAndGroup(); ok {
				blocks = append(blocks, Block{Type: "heading", Text: parseInline(title), Size: 3})
			}
		case p.startsWith(`\subsubsection`):
			flush()
			p.pos += len(`\subsubsection`)
			if title, ok := p.readOptionalStarAndGroup(); ok {
				blocks = append(blocks, Block{Type: "heading", Text: parseInline(title), Size: 4})
			}
		case p.startsWith(`\item`):
			flush()
			text.WriteString("• ")
			p.pos += len(`\item`)
			for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t') {
				p.pos++
			}
		case p.startsWith(`\begin{`), p.startsWith(`\end{`):
			environment, after, ok := readEnvironment(p.src, p.pos)
			if ok && wrapperEnvironments[environment] {
				flush()
				p.pos = after
			} else {
				text.WriteByte(p.src[p.pos])
				p.pos++
			}
		case p.startsWith(`\par`), p.startsWith(`\\`):
			flush()
			if p.startsWith(`\par`) {
				p.pos += len(`\par`)
			} else {
				p.pos += 2
			}
		case p.src[p.pos] == '\n' && p.blankLineAhead():
			flush()
			for !p.eof() && (p.src[p.pos] == '\n' || p.src[p.pos] == ' ' || p.src[p.pos] == '\t' || p.src[p.pos] == '\r') {
				p.pos++
			}
		default:
			text.WriteByte(p.src[p.pos])
			p.pos++
		}
	}
	flush()
	return blocks
}

// leadingSizeDeclaration strips a declaration-style font-size command at the
// beginning of a block and returns the closest supported rich heading size.
func leadingSizeDeclaration(source string) (content string, size int, ok bool) {
	source = strings.TrimSpace(source)
	if inner, after, grouped := readGroupAt(source, 0); grouped && after == len(source) {
		if content, size, ok = leadingSizeDeclaration(inner); ok {
			return content, size, true
		}
	}
	if !strings.HasPrefix(source, `\`) {
		return source, 0, false
	}
	name, next := readCommand(source, 0)
	size, ok = sizeCommands[name]
	if !ok {
		return source, 0, false
	}
	content = strings.TrimSpace(source[next:])
	if inner, after, grouped := readGroupAt(content, 0); grouped && after == len(content) {
		content = strings.TrimSpace(inner)
	}
	return content, size, true
}

// blankLineAhead reports whether the newline at the current position is
// followed by a blank line (two or more newlines, spaces allowed between).
func (p *parser) blankLineAhead() bool {
	index := p.pos + 1
	if index < len(p.src) && p.src[index] == '\r' {
		index++
	}
	for index < len(p.src) && (p.src[index] == ' ' || p.src[index] == '\t') {
		index++
	}
	return index < len(p.src) && p.src[index] == '\n'
}

func (p *parser) readUntil(terminator string) string {
	index := strings.Index(p.src[p.pos:], terminator)
	if index < 0 {
		result := p.src[p.pos:]
		p.pos = len(p.src)
		return result
	}
	result := p.src[p.pos : p.pos+index]
	p.pos += index + len(terminator)
	return result
}

// readOptionalStarAndGroup consumes an optional star and the following
// {group}; spaces are allowed before the brace.
func (p *parser) readOptionalStarAndGroup() (string, bool) {
	if p.pos < len(p.src) && p.src[p.pos] == '*' {
		p.pos++
	}
	return p.readGroup()
}

// readGroup reads a balanced {group} at the current position, skipping
// leading spaces.
func (p *parser) readGroup() (string, bool) {
	for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t') {
		p.pos++
	}
	if p.pos >= len(p.src) || p.src[p.pos] != '{' {
		return "", false
	}
	depth := 0
	start := p.pos + 1
	for index := p.pos; index < len(p.src); index++ {
		switch p.src[index] {
		case '\\':
			index++ // skip the escaped character
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				result := p.src[start:index]
				p.pos = index + 1
				return result, true
			}
		}
	}
	return "", false
}

// parseInline converts a text-mode fragment into a RichText value: a plain
// string when nothing needs formatting, or a slice of strings and inline
// nodes otherwise.
func parseInline(source string) any {
	var parts []any
	var text strings.Builder
	flush := func() {
		if text.Len() > 0 {
			parts = append(parts, text.String())
			text.Reset()
		}
	}
	appendParsed := func(value string) {
		flush()
		switch parsed := parseInline(value).(type) {
		case string:
			if parsed != "" {
				parts = append(parts, parsed)
			}
		case []any:
			parts = append(parts, parsed...)
		}
	}

	for index := 0; index < len(source); {
		char := source[index]
		switch {
		case char == '\\':
			if index+1 >= len(source) {
				text.WriteByte(char)
				index++
				continue
			}
			switch source[index+1] {
			case '&', '%', '$', '#', '_', '{', '}':
				text.WriteByte(source[index+1])
				index += 2
				continue
			case '\\':
				text.WriteByte('\n')
				index += 2
				continue
			case '(':
				end := strings.Index(source[index+2:], `\)`)
				if end >= 0 {
					expression := source[index+2 : index+2+end]
					if isSymbolsOnly(expression) {
						text.WriteString(strings.TrimSpace(expression))
					} else {
						flush()
						parts = append(parts, Inline{Type: "mathematical_expression", Expression: expression})
					}
					index += 2 + end + 2
					continue
				}
			}
			name, next := readCommand(source, index)
			if name == "" {
				text.WriteByte(char)
				index++
				continue
			}
			arg, after, hasArg := readGroupAt(source, next)
			if _, ok := sizeCommands[name]; ok {
				// Declarations are handled at block level when possible. Inside
				// another construct, discard the unsupported size change instead
				// of leaking the LaTeX command into the visible rich text.
				index = skipControlWordSpace(source, skipEmptyGroup(source, next))
				continue
			}
			switch name {
			case "textbf":
				if hasArg {
					flush()
					parts = append(parts, Inline{Type: "bold", Text: parseInline(arg)})
					index = after
					continue
				}
			case "textit", "emph":
				if hasArg {
					flush()
					parts = append(parts, Inline{Type: "italic", Text: parseInline(arg)})
					index = after
					continue
				}
			case "underline", "uline":
				if hasArg {
					flush()
					parts = append(parts, Inline{Type: "underline", Text: parseInline(arg)})
					index = after
					continue
				}
			case "texttt":
				if hasArg {
					flush()
					parts = append(parts, Inline{Type: "code", Text: parseInline(arg)})
					index = after
					continue
				}
			case "textsuperscript":
				if hasArg {
					flush()
					parts = append(parts, Inline{Type: "superscript", Text: parseInline(arg)})
					index = after
					continue
				}
			case "textsubscript":
				if hasArg {
					flush()
					parts = append(parts, Inline{Type: "subscript", Text: parseInline(arg)})
					index = after
					continue
				}
			case "textrm", "textsf", "textnormal", "textup", "textmd", "text":
				if hasArg {
					appendParsed(arg)
					index = after
					continue
				}
			case "textcolor":
				if _, afterColor, ok := readGroupAt(source, next); ok {
					if value, afterValue, ok := readGroupAt(source, afterColor); ok {
						appendParsed(value)
						index = afterValue
						continue
					}
				}
			case "href":
				if url, afterURL, ok := readGroupAt(source, next); ok {
					if label, afterLabel, ok := readGroupAt(source, afterURL); ok {
						flush()
						parts = append(parts, Inline{Type: "url", Text: parseInline(label), URL: url})
						index = afterLabel
						continue
					}
				}
			case "hyperlink", "hypertarget":
				// Telegram rich text has no named-anchor equivalent. Keep
				// the visible label and discard the internal PDF target; the
				// PNG/PDF renderer preserves the actual hyperref command.
				if _, afterTarget, ok := readGroupAt(source, next); ok {
					if label, afterLabel, ok := readGroupAt(source, afterTarget); ok {
						appendParsed(label)
						index = afterLabel
						continue
					}
				}
			case "url":
				if hasArg {
					flush()
					parts = append(parts, Inline{Type: "url", Text: arg, URL: arg})
					index = after
					continue
				}
			case "item":
				text.WriteString("• ")
				index = skipEmptyGroup(source, next)
				continue
			case "quad", "qquad", ",", ";", ":", "!", " ":
				text.WriteByte(' ')
				index = skipEmptyGroup(source, next)
				continue
			case "ldots", "dots":
				text.WriteString("…")
				index = skipEmptyGroup(source, next)
				continue
			case "LaTeX":
				text.WriteString("LaTeX")
				index = skipEmptyGroup(source, next)
				continue
			case "TeX":
				text.WriteString("TeX")
				index = skipEmptyGroup(source, next)
				continue
			}
			text.WriteString(`\` + name)
			index = next
		case char == '$':
			end := strings.IndexByte(source[index+1:], '$')
			if end >= 0 && end > 0 {
				expression := source[index+1 : index+1+end]
				if isSymbolsOnly(expression) {
					// Client math fonts often lack exotic symbols; the
					// regular text font renders them everywhere.
					text.WriteString(strings.TrimSpace(expression))
				} else {
					flush()
					parts = append(parts, Inline{Type: "mathematical_expression", Expression: expression})
				}
				index += end + 2
				continue
			}
			text.WriteByte(char)
			index++
		case char == '~':
			text.WriteByte(' ')
			index++
		default:
			text.WriteByte(char)
			index++
		}
	}
	flush()

	if len(parts) == 1 {
		if value, ok := parts[0].(string); ok {
			return value
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return parts
}

// skipControlWordSpace consumes whitespace following a control word, as TeX
// does for declaration commands such as \Huge and \small.
func skipControlWordSpace(source string, index int) int {
	for index < len(source) {
		switch source[index] {
		case ' ', '\t', '\r', '\n':
			index++
		default:
			return index
		}
	}
	return index
}

// skipEmptyGroup skips an empty {} group immediately following a control
// word, as in \LaTeX{}.
func skipEmptyGroup(source string, index int) int {
	if strings.HasPrefix(source[index:], "{}") {
		return index + 2
	}
	return index
}

// readCommand reads a backslash command name at position index.
func readCommand(source string, index int) (string, int) {
	next := index + 1
	name := ""
	for next < len(source) && isCommandLetter(source[next]) {
		name += string(source[next])
		next++
	}
	if name == "" && next < len(source) {
		// Single-character commands such as \, or \; .
		name = string(source[next])
		next++
	}
	return name, next
}

// readGroupAt reads a balanced {group} starting at position index, skipping
// spaces.
func readGroupAt(source string, index int) (string, int, bool) {
	for index < len(source) && (source[index] == ' ' || source[index] == '\t') {
		index++
	}
	if index >= len(source) || source[index] != '{' {
		return "", index, false
	}
	depth := 0
	start := index + 1
	for position := index; position < len(source); position++ {
		switch source[position] {
		case '\\':
			position++ // skip the escaped character
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return source[start:position], position + 1, true
			}
		}
	}
	return "", index, false
}
