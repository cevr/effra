package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

const FormatterSchemaVersion = 1

// FormatResult is the pure, syntax-only formatting snapshot. Its digest is
// intentionally independent from a checked semantic revision: formatting
// must work for unresolved imports and ill-typed but syntactically valid code.
type FormatResult struct {
	SchemaVersion int    `json:"schemaVersion"`
	InputDigest   string `json:"inputDigest"`
	OutputDigest  string `json:"outputDigest"`
	Changed       bool   `json:"changed"`
	Text          string `json:"text"`
}

// FormatFailure retains syntax diagnostics without returning replacement text.
type FormatFailure struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
}

func (e FormatFailure) Error() string {
	if len(e.Diagnostics) == 0 {
		return "source cannot be formatted"
	}
	return fmt.Sprintf("source cannot be formatted: %s", e.Diagnostics[0].Message)
}

func FormatSource(source string) (FormatResult, error) {
	inputDigest := formatDigest(source)
	program, tokens, diagnostics := parseSyntax(source)
	if len(diagnostics) > 0 {
		return FormatResult{SchemaVersion: FormatterSchemaVersion, InputDigest: inputDigest}, FormatFailure{Diagnostics: diagnostics}
	}
	if len(tokens) == 1 && tokens[0].kind == "eof" && len(program.Comments) == 0 {
		return FormatResult{SchemaVersion: FormatterSchemaVersion, InputDigest: inputDigest, OutputDigest: formatDigest(""), Changed: source != "", Text: ""}, nil
	}
	formatted := formatSyntax(source, program, tokens)
	return FormatResult{
		SchemaVersion: FormatterSchemaVersion,
		InputDigest:   inputDigest,
		OutputDigest:  formatDigest(formatted),
		Changed:       formatted != source,
		Text:          formatted,
	}, nil
}

func formatDigest(source string) string {
	digest := sha256.Sum256([]byte(source))
	return hex.EncodeToString(digest[:])
}

type formatEvent struct {
	offset     int
	end        int
	tokenIndex int
	comment    *Comment
}

type braceStyle uint8

const (
	braceBlock braceStyle = iota + 1
	braceInline
)

type formatLayout struct {
	breaks     map[int]bool
	inline     map[int]bool
	preserve   map[int]bool
	itemStarts map[int]bool
	braces     map[int]braceStyle
}

func formatSyntax(source string, program *Program, tokens []token) string {
	layout := buildFormatLayout(source, program, tokens)
	events := buildFormatEvents(program.Comments, tokens)
	printer := formatPrinter{source: source, tokens: tokens, events: events, layout: layout, lineStart: true, lastToken: -1}
	for index, event := range events {
		if event.comment != nil {
			printer.comment(event)
		} else {
			printer.token(index, event)
		}
	}
	return printer.finish()
}

func buildFormatEvents(comments []Comment, tokens []token) []formatEvent {
	events := make([]formatEvent, 0, len(comments)+len(tokens)-1)
	for index, current := range tokens {
		if current.kind != "eof" {
			events = append(events, formatEvent{offset: current.span.Offset, end: current.span.Offset + current.span.Length, tokenIndex: index})
		}
	}
	for index := range comments {
		comment := &comments[index]
		events = append(events, formatEvent{offset: comment.Span.Offset, end: comment.Span.Offset + comment.Span.Length, tokenIndex: -1, comment: comment})
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].offset != events[j].offset {
			return events[i].offset < events[j].offset
		}
		return events[i].tokenIndex < events[j].tokenIndex
	})
	return events
}

func buildFormatLayout(source string, program *Program, tokens []token) formatLayout {
	layout := formatLayout{breaks: map[int]bool{}, inline: map[int]bool{}, preserve: formatDirectiveTargetLines(program.Comments), itemStarts: map[int]bool{}, braces: map[int]braceStyle{}}
	for _, item := range program.Items {
		layout.itemStarts[item.Span.Offset] = true
		layout.breaks[item.Span.Offset] = true
	}
	for _, item := range program.Items {
		if item == nil {
			continue
		}
		switch item.Kind {
		case "error":
			collectFieldBreaks(&layout, item.Error.Fields)
		case "record":
			collectFieldBreaks(&layout, item.Record.Fields)
		case "enum":
			for _, variant := range item.Enum.Variants {
				layout.breaks[variant.Span.Offset] = true
				if !variant.Parenthesized {
					collectFieldBreaks(&layout, variant.Fields)
				}
			}
		case "service":
			for _, function := range item.Service.Methods {
				collectFunctionBreaks(&layout, function)
			}
		case "impl":
			for _, function := range item.Provider.Methods {
				collectFunctionBreaks(&layout, function)
			}
		case "function":
			collectFunctionBreaks(&layout, item.Function)
		}
	}
	stack := []int{}
	for index, current := range tokens {
		switch current.text {
		case "{":
			stack = append(stack, index)
		case "}":
			if len(stack) == 0 {
				continue
			}
			open := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			inline := isInlineBrace(source, program.Comments, tokens, open, index)
			if layout.preserve[tokens[open].span.Line] && open+1 < len(tokens) && tokens[open+1].span.Line == tokens[open].span.Line {
				inline = true
			}
			if inline {
				layout.braces[open] = braceInline
				layout.braces[index] = braceInline
			} else {
				layout.braces[open] = braceBlock
				layout.braces[index] = braceBlock
			}
		}
	}
	return layout
}

func collectFieldBreaks(layout *formatLayout, fields []Field) {
	for _, field := range fields {
		if !layout.preserve[field.Span.Line] {
			layout.breaks[field.Span.Offset] = true
		}
	}
}

func collectFunctionBreaks(layout *formatLayout, function *Function) {
	if function != nil {
		collectBlockBreaks(layout, function.Body)
	}
}

func collectBlockBreaks(layout *formatLayout, block *Block) {
	if block == nil {
		return
	}
	if block.Explicit {
		previousLine := 0
		for index, statement := range block.Statements {
			if index == 0 || statement.Span.Line != previousLine || !layout.preserve[statement.Span.Line] {
				layout.breaks[statement.Span.Offset] = true
			} else {
				// Statements sharing a physical source line stay together so a
				// next-line lint directive cannot be redirected by formatting.
				layout.inline[statement.Span.Offset] = true
			}
			previousLine = statement.Span.Line
			collectStatementBreaks(layout, statement)
		}
		return
	}
	for _, statement := range block.Statements {
		collectStatementBreaks(layout, statement)
	}
}

func formatDirectiveTargetLines(comments []Comment) map[int]bool {
	lines := map[int]bool{}
	for _, comment := range comments {
		body := strings.TrimLeft(comment.Text, " \t")
		if strings.HasPrefix(body, "effra-lint-disable-next-line") {
			lines[comment.Span.Line+1] = true
		}
	}
	return lines
}

func collectStatementBreaks(layout *formatLayout, statement *Statement) {
	if statement == nil {
		return
	}
	collectExpressionBreaks(layout, statement.Value)
	collectExpressionBreaks(layout, statement.Payload)
}

func collectExpressionBreaks(layout *formatLayout, expression *Expr) {
	if expression == nil {
		return
	}
	collectBlockBreaks(layout, expression.Then)
	collectBlockBreaks(layout, expression.Else)
	collectExpressionBreaks(layout, expression.Left)
	collectExpressionBreaks(layout, expression.Right)
	for _, argument := range expression.Args {
		collectExpressionBreaks(layout, argument)
	}
	for _, field := range expression.Fields {
		if expression.Kind != "call" && !layout.preserve[field.Span.Line] {
			layout.breaks[field.Span.Offset] = true
		}
		collectExpressionBreaks(layout, field.Value)
	}
	for _, arm := range expression.Arms {
		if arm.Pattern != nil && !layout.preserve[arm.Pattern.Span.Line] {
			layout.breaks[arm.Pattern.Span.Offset] = true
		}
		collectBlockBreaks(layout, arm.Body)
	}
}

func isInlineBrace(source string, comments []Comment, tokens []token, open, close int) bool {
	if close == open+1 && !hasCommentBetween(comments, tokens[open].span.Offset, tokens[close].span.Offset) {
		return true
	}
	if open > 0 && (tokens[open-1].text == "raises" || tokens[open-1].text == "uses") {
		return true
	}
	return close+1 < len(tokens) && tokens[close+1].text == "=>"
}

func hasCommentBetween(comments []Comment, start, end int) bool {
	for _, comment := range comments {
		if comment.Span.Offset > start && comment.Span.Offset < end {
			return true
		}
	}
	return false
}

type formatDelimiter struct {
	text  string
	style braceStyle
}

type formatPrinter struct {
	source     string
	tokens     []token
	events     []formatEvent
	layout     formatLayout
	output     strings.Builder
	indent     int
	lineStart  bool
	lastByte   byte
	lastEvent  int
	hasEvent   bool
	lastToken  int
	delimiters []formatDelimiter
}

func (p *formatPrinter) comment(event formatEvent) {
	gap := p.eventGap(event)
	newlines := formatNewlineCount(gap)
	if !p.hasEvent {
		// Leading blank space is canonicalized away; the comment itself remains.
	} else if newlines == 0 {
		p.space()
	} else {
		p.breaks(newlines)
	}
	raw := p.source[event.offset:event.end]
	raw = strings.TrimSuffix(raw, "\r")
	p.write(raw)
	p.newline()
	p.rememberEvent(event)
}

func (p *formatPrinter) token(eventIndex int, event formatEvent) {
	current := p.tokens[event.tokenIndex]
	gap := p.eventGap(event)
	physicalBreak := formatNewlineCount(gap) > 0
	if physicalBreak {
		p.breaks(formatNewlineCount(gap))
	}
	if (p.layout.itemStarts[current.span.Offset] || p.layout.breaks[current.span.Offset]) && !p.layout.inline[current.span.Offset] {
		if !physicalBreak && !p.layout.preserve[current.span.Line] {
			p.breaks(1)
		}
	}
	if p.lastToken >= 0 && p.tokens[p.lastToken].text == "}" && p.layout.braces[p.lastToken] == braceBlock && !formatContinuation(current.text) && !p.layout.preserve[current.span.Line] {
		p.newline()
	}
	switch current.text {
	case "{":
		p.openBrace(eventIndex, event)
	case "}":
		p.closeBrace(event.tokenIndex, current)
	case "(":
		p.regularSpacing(current.text)
		p.write(current.text)
		p.delimiters = append(p.delimiters, formatDelimiter{text: "("})
	case ")":
		p.regularSpacing(current.text)
		p.write(current.text)
		p.popDelimiter("(")
	case ",":
		p.regularSpacing(current.text)
		p.write(current.text)
		if p.topDelimiterStyle() == braceBlock && !p.nextEventIsTrailingComment(eventIndex, event) && !p.nextEventIsPinnedSameLine(eventIndex) {
			p.newline()
		}
	case ";":
		p.regularSpacing(current.text)
		p.write(current.text)
		if !p.nextEventIsTrailingComment(eventIndex, event) && !p.nextEventIsInlineStatement(eventIndex) && !p.nextEventIsPinnedSameLine(eventIndex) {
			p.newline()
		}
	default:
		p.regularSpacing(current.text)
		p.write(current.text)
	}
	p.lastToken = event.tokenIndex
	p.rememberEvent(event)
}

func (p *formatPrinter) openBrace(eventIndex int, event formatEvent) {
	current := p.tokens[event.tokenIndex]
	style := p.layout.braces[event.tokenIndex]
	p.regularSpacing(current.text)
	p.write(current.text)
	p.delimiters = append(p.delimiters, formatDelimiter{text: "{", style: style})
	if style == braceBlock {
		p.indent++
		if p.nextEventIsTrailingComment(eventIndex, event) {
			p.space()
		} else {
			p.newline()
		}
	}
}

func (p *formatPrinter) closeBrace(tokenIndex int, current token) {
	style := p.layout.braces[tokenIndex]
	if style == braceBlock {
		if !p.layout.preserve[current.span.Line] {
			p.breaks(1)
		}
		if p.indent > 0 {
			p.indent--
		}
		p.write(current.text)
	} else {
		p.write(current.text)
	}
	p.popDelimiter("{")
}

func (p *formatPrinter) regularSpacing(current string) {
	if p.lineStart || p.lastToken < 0 {
		return
	}
	previous := p.tokens[p.lastToken].text
	if !formatNeedsSpace(previous, current) {
		return
	}
	p.space()
}

func formatNeedsSpace(previous, current string) bool {
	if current == ")" || current == "]" || current == "," || current == ";" || current == "." || current == ":" || current == ">" {
		return false
	}
	if previous == "(" || previous == "." || previous == "<" || previous == "{" {
		return false
	}
	if current == "(" {
		switch previous {
		case "->", "=", "==", "+", "if", "match", "run", "fork":
			return true
		default:
			return false
		}
	}
	if current == "<" {
		return false
	}
	if previous == ":" {
		return true
	}
	return true
}

func formatContinuation(current string) bool {
	switch current {
	case "else", ")", "]", ",", ";", "}", ".", ">":
		return true
	default:
		return false
	}
}

func (p *formatPrinter) topDelimiterStyle() braceStyle {
	if len(p.delimiters) == 0 {
		return 0
	}
	return p.delimiters[len(p.delimiters)-1].style
}

func (p *formatPrinter) popDelimiter(text string) {
	for index := len(p.delimiters) - 1; index >= 0; index-- {
		if p.delimiters[index].text == text {
			p.delimiters = p.delimiters[:index]
			return
		}
	}
}

func (p *formatPrinter) nextEventIsTrailingComment(eventIndex int, event formatEvent) bool {
	if eventIndex+1 >= len(p.events) || p.events[eventIndex+1].comment == nil {
		return false
	}
	return formatNewlineCount(p.source[event.end:p.events[eventIndex+1].offset]) == 0
}

func (p *formatPrinter) nextEventIsInlineStatement(eventIndex int) bool {
	if eventIndex+1 >= len(p.events) || p.events[eventIndex+1].comment != nil {
		return false
	}
	next := p.tokens[p.events[eventIndex+1].tokenIndex]
	return p.layout.inline[next.span.Offset]
}

func (p *formatPrinter) nextEventIsPinnedSameLine(eventIndex int) bool {
	if eventIndex+1 >= len(p.events) || p.events[eventIndex+1].comment != nil {
		return false
	}
	next := p.tokens[p.events[eventIndex+1].tokenIndex]
	if !p.layout.preserve[next.span.Line] {
		return false
	}
	return p.tokens[p.events[eventIndex].tokenIndex].span.Line == next.span.Line
}

func (p *formatPrinter) eventGap(event formatEvent) string {
	if !p.hasEvent || event.offset < p.lastEvent {
		return ""
	}
	return p.source[p.lastEvent:event.offset]
}

func (p *formatPrinter) rememberEvent(event formatEvent) {
	p.lastEvent = event.end
	p.hasEvent = true
}

func (p *formatPrinter) breaks(count int) {
	if count < 1 {
		return
	}
	if count > 2 {
		count = 2
	}
	if !p.lineStart {
		p.newline()
		count--
	}
	for count > 1 {
		p.output.WriteByte('\n')
		p.lineStart = true
		p.lastByte = '\n'
		count--
	}
}

func (p *formatPrinter) newline() {
	if p.lineStart {
		return
	}
	p.output.WriteByte('\n')
	p.lineStart = true
	p.lastByte = '\n'
}

func (p *formatPrinter) space() {
	if p.lineStart || p.output.Len() == 0 {
		return
	}
	if p.lastByte != ' ' && p.lastByte != '\n' && p.lastByte != '\t' {
		p.output.WriteByte(' ')
		p.lastByte = ' '
	}
}

func (p *formatPrinter) write(text string) {
	if text == "" {
		return
	}
	if p.lineStart {
		p.output.WriteString(strings.Repeat("    ", p.indent))
		p.lineStart = false
	}
	p.output.WriteString(text)
	p.lastByte = text[len(text)-1]
}

func (p *formatPrinter) finish() string {
	if p.output.Len() == 0 {
		return ""
	}
	formatted := strings.TrimRight(p.output.String(), "\n")
	return formatted + "\n"
}

func formatNewlineCount(gap string) int {
	return strings.Count(gap, "\n")
}
