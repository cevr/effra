package effra

import (
	"fmt"
	"strconv"
	"strings"
)

// Entry failure report: the target-neutral text a program entry writes to
// stderr when main's exit is a failure. docs/design.md ("Entry failure
// report") is the contract, and the JavaScript entry
// (internal/compiler/prelude/entry.mjs) renders the same bytes.
//
//	failure: Tag
//	failure: Tag { field: value, ... }
//	defect: "message"
//	interrupt
//
// One line per cause reason, in cause order. The compiler generates the
// EntryReportPlan from main's checked failure row, so values are rendered by
// their checked type, never by their runtime shape.

// ReportKind is the checked rendering kind of one plan node.
type ReportKind uint8

const (
	ReportOpaque ReportKind = iota
	ReportString
	ReportI64
	ReportBool
	ReportVoid
	ReportBytes
	ReportFn
	ReportRecord
	ReportEnum
)

// ReportField reads one declared field, listed in byte order of the names.
type ReportField struct {
	Name string
	Node int
	Get  func(any) any
}

// ReportVariant matches one enum alternative and returns its value.
type ReportVariant struct {
	Name   string
	Match  func(any) (any, bool)
	Fields []ReportField
}

type ReportNode struct {
	Kind     ReportKind
	Fields   []ReportField
	Variants []ReportVariant
}

// ReportFailure describes one failure label. A diagnostic failure is a
// built-in failure whose payload is host diagnostic text.
type ReportFailure struct {
	Diagnostic bool
	Fields     []ReportField
}

type EntryReportPlan struct {
	Failures map[string]ReportFailure
	Nodes    []ReportNode
}

// EntryExitCode is the process exit status for a failed entry: 130 when every
// reason is an interruption, as for a shell's SIGINT, and 1 otherwise.
func EntryExitCode(cause Cause) int {
	if cause.OnlyInterrupts() {
		return 130
	}
	return 1
}

// Report renders every reason of a failed entry's cause.
func (p EntryReportPlan) Report(cause Cause) string {
	var out strings.Builder
	for _, reason := range cause {
		switch {
		case reason.Failure != nil:
			out.WriteString("failure: " + reason.Failure.Tag)
			p.payload(&out, reason.Failure)
		case reason.Kind == "defect":
			message := "<nil>"
			if reason.Err != nil {
				message = reason.Err.Error()
			}
			out.WriteString("defect: " + QuoteText(message))
		default:
			out.WriteString(reason.Kind)
		}
		out.WriteByte('\n')
	}
	return out.String()
}

func (p EntryReportPlan) payload(out *strings.Builder, failure *Failure) {
	described, ok := p.Failures[failure.Tag]
	if !ok {
		return
	}
	if described.Diagnostic {
		var message string
		switch text := failure.Payload.(type) {
		case string:
			message = text
		case error:
			message = text.Error()
		default:
			return
		}
		out.WriteString(" { message: " + QuoteText(message) + " }")
		return
	}
	if len(described.Fields) > 0 && failure.Payload != nil {
		out.WriteByte(' ')
		p.fields(out, failure.Payload, described.Fields)
	}
}

func (p EntryReportPlan) fields(out *strings.Builder, value any, fields []ReportField) {
	if len(fields) == 0 {
		out.WriteString("{}")
		return
	}
	out.WriteString("{ ")
	for i, field := range fields {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(field.Name + ": ")
		read, ok := readField(field, value)
		if !ok {
			out.WriteString("<opaque>")
			continue
		}
		p.value(out, read, field.Node)
	}
	out.WriteString(" }")
}

// readField applies a generated accessor. A mismatch would be a compiler
// defect; it renders as opaque rather than losing the rest of the report.
func readField(field ReportField, value any) (result any, ok bool) {
	defer func() {
		if recover() != nil {
			result, ok = nil, false
		}
	}()
	return field.Get(value), true
}

func (p EntryReportPlan) value(out *strings.Builder, value any, index int) {
	if index < 0 || index >= len(p.Nodes) {
		out.WriteString("<opaque>")
		return
	}
	node := p.Nodes[index]
	switch node.Kind {
	case ReportString:
		if text, ok := value.(string); ok {
			out.WriteString(QuoteText(text))
			return
		}
	case ReportI64:
		if number, ok := value.(int64); ok {
			out.WriteString(strconv.FormatInt(number, 10))
			return
		}
	case ReportBool:
		if flag, ok := value.(bool); ok {
			out.WriteString(strconv.FormatBool(flag))
			return
		}
	case ReportVoid:
		out.WriteString("void")
		return
	case ReportBytes:
		if data, ok := value.([]byte); ok {
			out.WriteString("<bytes len=" + strconv.Itoa(len(data)) + ">")
			return
		}
	case ReportFn:
		out.WriteString("<fn>")
		return
	case ReportRecord:
		p.fields(out, value, node.Fields)
		return
	case ReportEnum:
		for _, variant := range node.Variants {
			if alternative, ok := variant.Match(value); ok {
				out.WriteString(variant.Name)
				if len(variant.Fields) > 0 {
					out.WriteByte(' ')
					p.fields(out, alternative, variant.Fields)
				}
				return
			}
		}
	}
	out.WriteString("<opaque>")
}

// QuoteText is the one quoting rule for text in diagnostics and reports,
// shared byte for byte with the JavaScript runtime (__ef_quoteText): `"` and
// `\` are escaped, newline, carriage return and tab use their short escapes,
// other C0 controls and DEL use \u00XX, and every other character is
// literal. Invalid UTF-8 is replaced by U+FFFD, as JavaScript replaces lone
// surrogates.
func QuoteText(text string) string {
	var out strings.Builder
	out.WriteByte('"')
	for _, r := range text {
		switch r {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		case '\n':
			out.WriteString(`\n`)
		case '\r':
			out.WriteString(`\r`)
		case '\t':
			out.WriteString(`\t`)
		default:
			if r < 0x20 || r == 0x7f {
				fmt.Fprintf(&out, `\u%04x`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
	return out.String()
}
