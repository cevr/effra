package effra

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Entry failure report: the target-neutral text a program entry writes to
// stderr when main's exit is a failure. The JavaScript entry
// (internal/compiler/prelude/entry.mjs) implements the same grammar byte for
// byte; docs/design.md ("Failure is more than a Result") is the contract.
//
//	failure: Tag
//	failure: Tag { field: value, ... }
//	defect: "message"
//	interrupt
//
// One line per cause reason, in cause order. Payload fields are listed in
// byte order of their source names, because the JavaScript representation
// does not retain declaration order.

// EntryExitCode is the process exit status for a failed entry: 130 when every
// reason is an interruption, as for a shell's SIGINT, and 1 otherwise.
func EntryExitCode(cause Cause) int {
	if cause.OnlyInterrupts() {
		return 130
	}
	return 1
}

// EntryReport renders every reason of a failed entry's cause.
func EntryReport(cause Cause) string {
	var out strings.Builder
	for _, reason := range cause {
		switch {
		case reason.Failure != nil:
			out.WriteString("failure: " + reason.Failure.Tag)
			reportPayload(&out, reason.Failure.Payload)
		case reason.Kind == "defect":
			out.WriteString("defect: ")
			message := "<nil>"
			if reason.Err != nil {
				message = reason.Err.Error()
			}
			reportString(&out, message)
		default:
			out.WriteString(reason.Kind)
		}
		out.WriteByte('\n')
	}
	return out.String()
}

// reportPayload writes a failure payload's fields after its tag. Built-in
// failures that carry host diagnostic text (a string or a Go error) report it
// as their message field, as the JavaScript providers construct it.
func reportPayload(out *strings.Builder, payload any) {
	switch value := payload.(type) {
	case nil:
		return
	case string:
		out.WriteString(" { message: ")
		reportString(out, value)
		out.WriteString(" }")
		return
	case error:
		out.WriteString(" { message: ")
		reportString(out, value.Error())
		out.WriteString(" }")
		return
	}
	v := reflect.ValueOf(payload)
	if fields, ok := generatedFields(v); ok {
		if len(fields) > 0 {
			out.WriteByte(' ')
			reportFields(out, fields)
		}
		return
	}
	out.WriteByte(' ')
	reportValue(out, v)
}

type reportField struct {
	name  string
	value reflect.Value
}

// generatedFields decodes a generated record, error or variant struct. Its
// fields use goFieldName's injective encoding, EfField_<length>_<name>.
func generatedFields(v reflect.Value) ([]reportField, bool) {
	if v.Kind() != reflect.Struct || v.Type().Name() == "" {
		return nil, false
	}
	fields := []reportField{}
	for i := 0; i < v.NumField(); i++ {
		name, ok := strings.CutPrefix(v.Type().Field(i).Name, "EfField_")
		if !ok {
			return nil, false
		}
		name, ok = lengthPrefixed(name)
		if !ok {
			return nil, false
		}
		fields = append(fields, reportField{name, v.Field(i)})
	}
	sort.Slice(fields, func(i, j int) bool { return fields[i].name < fields[j].name })
	return fields, true
}

// lengthPrefixed reads "<n>_<name...>" and returns the first n bytes of name.
func lengthPrefixed(encoded string) (string, bool) {
	digits, rest, ok := strings.Cut(encoded, "_")
	n, err := strconv.Atoi(digits)
	if !ok || err != nil || n < 0 || n > len(rest) {
		return "", false
	}
	return rest[:n], true
}

// variantName decodes goVariantType's encoding,
// efTypeV_<length>_<owner>_<length>_<variant>, ignoring any instantiation.
func variantName(t reflect.Type) (string, bool) {
	encoded, ok := strings.CutPrefix(t.Name(), "efTypeV_")
	if !ok {
		return "", false
	}
	owner, ok := lengthPrefixed(encoded)
	if !ok {
		return "", false
	}
	_, after, _ := strings.Cut(encoded, "_")
	rest, ok := strings.CutPrefix(after[len(owner):], "_")
	if !ok {
		return "", false
	}
	return lengthPrefixed(rest)
}

func reportFields(out *strings.Builder, fields []reportField) {
	out.WriteString("{ ")
	for i, field := range fields {
		if i > 0 {
			out.WriteString(", ")
		}
		out.WriteString(field.name + ": ")
		reportValue(out, field.value)
	}
	out.WriteString(" }")
}

// reportValue renders one payload value in the canonical grammar shared with
// the JavaScript entry. Values with no portable data rendering are opaque.
func reportValue(out *strings.Builder, v reflect.Value) {
	for v.Kind() == reflect.Interface && !v.IsNil() {
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.String:
		reportString(out, v.String())
		return
	case reflect.Int64:
		out.WriteString(strconv.FormatInt(v.Int(), 10))
		return
	case reflect.Bool:
		out.WriteString(strconv.FormatBool(v.Bool()))
		return
	case reflect.Func:
		out.WriteString("<fn>")
		return
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			out.WriteString("<bytes len=" + strconv.Itoa(v.Len()) + ">")
			return
		}
	case reflect.Struct:
		if v.Type().Name() == "" && v.NumField() == 0 {
			out.WriteString("void")
			return
		}
		if fields, ok := generatedFields(v); ok {
			if variant, ok := variantName(v.Type()); ok {
				out.WriteString(variant)
				if len(fields) > 0 {
					out.WriteByte(' ')
					reportFields(out, fields)
				}
				return
			}
			if len(fields) == 0 {
				out.WriteString("{}")
				return
			}
			reportFields(out, fields)
			return
		}
	}
	out.WriteString("<opaque>")
}

// reportString quotes text: `"` and `\` are escaped, newline, carriage return
// and tab use their short escapes, and other C0 controls and DEL use \u00XX.
// Invalid UTF-8 is replaced by U+FFFD, as JavaScript replaces lone surrogates.
func reportString(out *strings.Builder, text string) {
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
				fmt.Fprintf(out, `\u%04x`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
}
