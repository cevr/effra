package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The snapshot's canonical bytes: two-space indentation, ": " and "," separators, raw non-ASCII text and
// only the escapes JSON requires. The format is the published contract of docs/wayfinder/snapshot.json, so
// it is written by hand rather than through encoding/json, whose escaping rules differ (HTML characters,
// U+2028/U+2029, \b and \f).

// field and object keep insertion order for output whose key order is part of the interface (--json).
type field struct {
	key   string
	value any
}

type object []field

var integerText = regexp.MustCompile(`^-?[0-9]+$`)

// renderJSON writes value with two-space indentation. sortKeys orders every object's keys; parsed objects
// (map[string]any) are always sorted.
func renderJSON(value any, sortKeys bool) string {
	var out strings.Builder
	writeJSON(&out, value, sortKeys, 0)
	return out.String()
}

func writeJSON(out *strings.Builder, value any, sortKeys bool, level int) {
	switch v := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		if v {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case int:
		out.WriteString(strconv.Itoa(v))
	case json.Number:
		text := string(v)
		if integerText.MatchString(text) {
			if parsed, err := strconv.ParseInt(text, 10, 64); err == nil {
				text = strconv.FormatInt(parsed, 10)
			}
		}
		out.WriteString(text)
	case string:
		writeString(out, v)
	case []any:
		if len(v) == 0 {
			out.WriteString("[]")
			return
		}
		out.WriteString("[")
		for index, item := range v {
			if index > 0 {
				out.WriteString(",")
			}
			newline(out, level+1)
			writeJSON(out, item, sortKeys, level+1)
		}
		newline(out, level)
		out.WriteString("]")
	case map[string]any:
		fields := make(object, 0, len(v))
		for key, item := range v {
			fields = append(fields, field{key, item})
		}
		writeObject(out, fields, true, level)
	case object:
		writeObject(out, v, sortKeys, level)
	default:
		panic(fmt.Sprintf("wayfinder: cannot render %T", value))
	}
}

func writeObject(out *strings.Builder, fields object, sortKeys bool, level int) {
	if len(fields) == 0 {
		out.WriteString("{}")
		return
	}
	if sortKeys {
		fields = append(object(nil), fields...)
		sort.SliceStable(fields, func(i, j int) bool { return fields[i].key < fields[j].key })
	}
	out.WriteString("{")
	for index, item := range fields {
		if index > 0 {
			out.WriteString(",")
		}
		newline(out, level+1)
		writeString(out, item.key)
		out.WriteString(": ")
		writeJSON(out, item.value, sortKeys, level+1)
	}
	newline(out, level)
	out.WriteString("}")
}

func newline(out *strings.Builder, level int) {
	out.WriteString("\n")
	out.WriteString(strings.Repeat("  ", level))
}

func writeString(out *strings.Builder, text string) {
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
		case '\b':
			out.WriteString(`\b`)
		case '\f':
			out.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(out, `\u%04x`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
}

// pyList formats strings the way the Python CLI printed a list in messages: ['a', 'b'].
func pyList(values []string) string {
	quoted := make([]string, len(values))
	for index, value := range values {
		quoted[index] = "'" + value + "'"
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func intList(values []int) string {
	text := make([]string, len(values))
	for index, value := range values {
		text[index] = strconv.Itoa(value)
	}
	return "[" + strings.Join(text, ", ") + "]"
}
