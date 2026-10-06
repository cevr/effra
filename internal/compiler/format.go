package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

const FormatterSchemaVersion = 1

// FormatterIdentity names the syntax producer independently from semantic
// revisions. Adapters must report this identity without querying Git.
const FormatterIdentity = "effra/formatter-schema-1"

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
	return formatSource(source, 0)
}

// FormatLimitError means that the complete replacement would exceed the
// caller-provided output bound. It never carries partial replacement text.
type FormatLimitError struct {
	Limit int
}

func (e FormatLimitError) Error() string {
	return fmt.Sprintf("formatted source exceeds %d-byte limit", e.Limit)
}

// FormatSourceBounded runs the same syntax-only formatter while bounding the
// complete formatted document. A non-positive bound preserves the unbounded
// core behavior; adapters should provide an explicit positive bound.
func FormatSourceBounded(source string, maxOutputBytes int) (FormatResult, error) {
	if maxOutputBytes < 0 {
		return FormatResult{}, fmt.Errorf("formatter output limit must not be negative")
	}
	return formatSource(source, maxOutputBytes)
}

func formatSource(source string, maxOutputBytes int) (FormatResult, error) {
	inputDigest := formatDigest(source)
	program, tokens, diagnostics := parseSyntax(source)
	if len(diagnostics) > 0 {
		return FormatResult{SchemaVersion: FormatterSchemaVersion, InputDigest: inputDigest}, FormatFailure{Diagnostics: diagnostics}
	}
	if len(tokens) == 1 && tokens[0].kind == "eof" && len(program.Comments) == 0 {
		return FormatResult{SchemaVersion: FormatterSchemaVersion, InputDigest: inputDigest, OutputDigest: formatDigest(""), Changed: source != "", Text: ""}, nil
	}
	formatted, err := formatSyntaxBounded(source, program, tokens, maxOutputBytes)
	if err != nil {
		return FormatResult{SchemaVersion: FormatterSchemaVersion, InputDigest: inputDigest}, err
	}
	return FormatResult{
		SchemaVersion: FormatterSchemaVersion,
		InputDigest:   inputDigest,
		OutputDigest:  formatDigest(formatted),
		Changed:       formatted != source,
		Text:          formatted,
	}, nil
}

// FormatDigest returns the exact source-byte digest used by FormatSource.
// It lets source-buffer adapters reject stale input before formatting.
func FormatDigest(source string) string {
	return formatDigest(source)
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
	spaced     map[int]bool
}

func formatSyntax(source string, program *Program, tokens []token) string {
	formatted, _ := formatSyntaxBounded(source, program, tokens, 0)
	return formatted
}

func formatSyntaxBounded(source string, program *Program, tokens []token, maxOutputBytes int) (string, error) {
	layout := buildFormatLayout(source, program, tokens)
	events := buildFormatEvents(program.Comments, tokens)
	printer := formatPrinter{source: source, tokens: tokens, events: events, layout: layout, maxOutputBytes: maxOutputBytes, lineStart: true, lastToken: -1}
	for index, event := range events {
		if event.comment != nil {
			printer.comment(event)
		} else {
			printer.token(index, event)
		}
	}
	if printer.outputLimitExceeded {
		return "", FormatLimitError{Limit: maxOutputBytes}
	}
	formatted, ok := printer.finish()
	if !ok {
		return "", FormatLimitError{Limit: maxOutputBytes}
	}
	return formatted, nil
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
	layout := formatLayout{breaks: map[int]bool{}, inline: map[int]bool{}, preserve: formatDirectiveTargetLines(program.Comments), itemStarts: map[int]bool{}, braces: map[int]braceStyle{}, spaced: map[int]bool{}}
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
				if index > open+1 {
					layout.spaced[open] = true
					layout.spaced[index] = true
				}
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
	if function == nil {
		return
	}
	if function.DeclSpan.Length > 0 {
		layout.breaks[function.DeclSpan.Offset] = true
	}
	collectBlockBreaks(layout, function.Body)
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
		if isSuppressionComment(comment.Text) {
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
	if expression.Kind != "call" {
		for _, field := range expression.Fields {
			if !layout.preserve[field.Span.Line] {
				layout.breaks[field.Span.Offset] = true
			}
		}
	}
	forEachExprChild(expression, func(child *Expr) {
		collectExpressionBreaks(layout, child)
	})
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
	index := sort.Search(len(comments), func(index int) bool {
		return comments[index].Span.Offset > start
	})
	return index < len(comments) && comments[index].Span.Offset < end
}

type formatDelimiter struct {
	text  string
	style braceStyle
	base  int
}

type formatPrinter struct {
	source              string
	tokens              []token
	events              []formatEvent
	layout              formatLayout
	output              strings.Builder
	indent              int
	lineIndent          int
	trailingNewlines    int
	lineStart           bool
	lastByte            byte
	lastEvent           int
	hasEvent            bool
	lastToken           int
	delimiters          []formatDelimiter
	maxOutputBytes      int
	outputLimitExceeded bool
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
	if p.lineStart {
		p.prepareCommentLine()
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
	if p.lineStart {
		p.prepareTokenLine(current, event.tokenIndex)
	}
	switch current.text {
	case "{":
		p.openBrace(eventIndex, event)
	case "}":
		p.closeBrace(event.tokenIndex, current)
	case "(":
		p.regularSpacing(current.text)
		p.write(current.text)
		p.delimiters = append(p.delimiters, formatDelimiter{text: "(", base: p.lineIndent})
	case ")":
		p.regularSpacing(current.text)
		p.write(current.text)
		p.popDelimiter("(")
	case "<":
		p.regularSpacing(current.text)
		p.write(current.text)
		p.delimiters = append(p.delimiters, formatDelimiter{text: "<", base: p.lineIndent})
	case ">":
		p.regularSpacing(current.text)
		p.write(current.text)
		p.popDelimiter("<")
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
	p.delimiters = append(p.delimiters, formatDelimiter{text: "{", style: style, base: p.lineIndent})
	if style == braceBlock {
		p.indent++
		if p.nextEventIsTrailingComment(eventIndex, event) {
			p.space()
		} else {
			p.newline()
		}
	} else if p.layout.spaced[event.tokenIndex] && p.nextEventIsSameLine(eventIndex, event) {
		p.space()
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
		p.lineIndent = p.indent + p.continuationDepth()
		p.write(current.text)
	} else {
		if p.layout.spaced[tokenIndex] && !p.lineStart {
			p.space()
		}
		p.write(current.text)
	}
	p.popDelimiter("{")
}

func (p *formatPrinter) regularSpacing(current string) {
	if p.lineStart || p.lastToken < 0 {
		return
	}
	previous := p.tokens[p.lastToken]
	currentToken := token{text: current}
	if !formatNeedsSpace(previous, currentToken) {
		return
	}
	p.space()
}

func formatNeedsSpace(previous, current token) bool {
	previousText, currentText := previous.text, current.text
	if currentText == ")" || currentText == "]" || currentText == "," || currentText == ";" || currentText == "." || currentText == ":" || currentText == ">" {
		return false
	}
	if previousText == "(" || previousText == "." || previousText == "<" || previousText == "{" {
		return false
	}
	if currentText == "(" {
		switch previousText {
		case ")", "]", ">", "(":
			return false
		default:
			if previous.kind == "name" && !groupedCallKeyword(previousText) {
				return false
			}
			return true
		}
	}
	if currentText == "<" {
		return false
	}
	if previousText == ":" {
		return true
	}
	return true
}

func groupedCallKeyword(text string) bool {
	switch text {
	case "if", "match", "run", "fork":
		return true
	default:
		return false
	}
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

func (p *formatPrinter) continuationDepth() int {
	depth := 0
	for _, delimiter := range p.delimiters {
		if delimiter.style != braceBlock {
			depth++
		}
	}
	return depth
}

func (p *formatPrinter) prepareCommentLine() {
	p.lineIndent = p.continuationContentIndent()
}

func (p *formatPrinter) topContinuationBase() (int, bool) {
	for index := len(p.delimiters) - 1; index >= 0; index-- {
		if p.delimiters[index].style != braceBlock {
			return p.delimiters[index].base, true
		}
	}
	return 0, false
}

func (p *formatPrinter) continuationContentIndent() int {
	indent := p.indent + p.continuationDepth()
	if base, ok := p.topContinuationBase(); ok && indent < base+1 {
		indent = base + 1
	}
	return indent
}

func (p *formatPrinter) delimiterBase(text string) (int, bool) {
	switch text {
	case ")":
		text = "("
	case "]":
		text = "["
	case ">":
		text = "<"
	case "}":
		text = "{"
	}
	for index := len(p.delimiters) - 1; index >= 0; index-- {
		if p.delimiters[index].text == text {
			return p.delimiters[index].base, true
		}
	}
	return 0, false
}

func (p *formatPrinter) prepareTokenLine(current token, tokenIndex int) {
	depth := p.continuationDepth()
	closing := current.text == ")" || current.text == "]" || current.text == ">" || (current.text == "}" && p.layout.braces[tokenIndex] == braceInline)
	if closing {
		if base, ok := p.delimiterBase(current.text); ok {
			p.lineIndent = base
		} else {
			if depth > 0 {
				depth--
			}
			p.lineIndent = p.indent + depth
		}
	} else {
		p.lineIndent = p.indent + depth
		if base, ok := p.topContinuationBase(); ok && p.lineIndent < base+1 {
			p.lineIndent = base + 1
		}
	}
	if current.text == "else" {
		p.lineIndent = p.indent
		return
	}
	if current.text == "{" && p.layout.braces[tokenIndex] == braceBlock {
		p.lineIndent = p.indent + p.continuationDepth()
		return
	}
	if closing {
		return
	}
	if p.layout.itemStarts[current.span.Offset] || (p.layout.breaks[current.span.Offset] && !p.layout.inline[current.span.Offset]) {
		return
	}
	if depth == 0 {
		p.lineIndent++
	}
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

func (p *formatPrinter) nextEventIsSameLine(eventIndex int, event formatEvent) bool {
	if eventIndex+1 >= len(p.events) {
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
	if count < 1 || p.output.Len() == 0 {
		return
	}
	if count > 2 {
		count = 2
	}
	for p.trailingNewlines < count {
		if !p.writeByte('\n') {
			return
		}
		p.lineStart = true
		p.lastByte = '\n'
		p.trailingNewlines++
	}
}

func (p *formatPrinter) newline() {
	if p.output.Len() == 0 || p.trailingNewlines >= 1 {
		return
	}
	if !p.writeByte('\n') {
		return
	}
	p.lineStart = true
	p.lastByte = '\n'
	p.trailingNewlines = 1
}

func (p *formatPrinter) space() {
	if p.lineStart || p.output.Len() == 0 {
		return
	}
	if p.lastByte != ' ' && p.lastByte != '\n' && p.lastByte != '\t' {
		if !p.writeByte(' ') {
			return
		}
		p.lastByte = ' '
	}
}

func (p *formatPrinter) write(text string) {
	if text == "" {
		return
	}
	indentBytes := 0
	if p.lineStart {
		indentBytes = p.lineIndent * 4
		if p.maxOutputBytes > 0 && (indentBytes < 0 || p.output.Len()+indentBytes+len(text) > p.maxOutputBytes) {
			p.outputLimitExceeded = true
			return
		}
		p.output.WriteString(strings.Repeat("    ", p.lineIndent))
		p.lineStart = false
	}
	if p.maxOutputBytes > 0 && p.output.Len()+len(text) > p.maxOutputBytes {
		p.outputLimitExceeded = true
		return
	}
	p.output.WriteString(text)
	p.lastByte = text[len(text)-1]
	p.trailingNewlines = 0
}

func (p *formatPrinter) writeByte(value byte) bool {
	if p.maxOutputBytes > 0 && p.output.Len()+1 > p.maxOutputBytes {
		p.outputLimitExceeded = true
		return false
	}
	p.output.WriteByte(value)
	return true
}

func (p *formatPrinter) finish() (string, bool) {
	if p.output.Len() == 0 {
		return "", true
	}
	formatted := strings.TrimRight(p.output.String(), "\n")
	if p.maxOutputBytes > 0 && len(formatted)+1 > p.maxOutputBytes {
		return "", false
	}
	return formatted + "\n", true
}

func formatNewlineCount(gap string) int {
	return strings.Count(gap, "\n")
}
