package main

// Byte-for-byte ports of the Python standard-library behavior the original
// scripts relied on: json.loads/json.dumps, repr of JSON values, Path.read_text
// newline translation, str.splitlines, str.isspace and shlex.join. The
// committed manifest and its sha256 identities were produced by Python, so
// every encoding here must match it exactly.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// errNotText reports a file that is not readable UTF-8 text.
var errNotText = fmt.Errorf("not UTF-8 text")

// readText reads a file like Python's Path.read_text(encoding="utf-8"):
// strict UTF-8 and universal newlines (\r\n and \r become \n).
func readText(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", errNotText
	}
	return translateNewlines(string(data)), nil
}

func translateNewlines(text string) string {
	if !strings.Contains(text, "\r") {
		return text
	}
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
}

// decodeJSON decodes like Python's json.loads into generic values whose
// numbers stay json.Number, so integers and floats remain distinct types.
func decodeJSON(text string) (any, error) {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err == nil {
		return nil, fmt.Errorf("extra data after JSON value")
	}
	return value, nil
}

// jsonInt returns the integer a json.Number holds when Python would decode
// it as int (no fraction or exponent).
func jsonInt(number json.Number) (*big.Int, bool) {
	if strings.ContainsAny(string(number), ".eE") {
		return nil, false
	}
	value, ok := new(big.Int).SetString(string(number), 10)
	return value, ok
}

func jsonFloat(number json.Number) float64 {
	value, _ := strconv.ParseFloat(string(number), 64)
	return value
}

// pyType names the Python type json.loads would produce for a value.
func pyType(value any) string {
	switch v := value.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case string:
		return "str"
	case json.Number:
		if _, ok := jsonInt(v); ok {
			return "int"
		}
		return "float"
	case []any:
		return "list"
	case map[string]any:
		return "dict"
	}
	panic(fmt.Sprintf("unsupported JSON value %T", value))
}

// pyScalarEqual is Python's == on two scalars of the same pyType.
func pyScalarEqual(left, right any) bool {
	if l, ok := left.(json.Number); ok {
		r := right.(json.Number)
		if li, ok := jsonInt(l); ok {
			ri, _ := jsonInt(r)
			return li.Cmp(ri) == 0
		}
		return jsonFloat(l) == jsonFloat(r)
	}
	return left == right
}

// canonicalJSON is json.dumps(value, ensure_ascii=False, sort_keys=True,
// separators=(",", ":")).encode("utf-8").
func canonicalJSON(value any) []byte {
	var out bytes.Buffer
	encodePy(&out, value, false, -1, 0)
	return out.Bytes()
}

// indentedJSON is json.dumps(value, indent=2, sort_keys=True), whose
// ensure_ascii default escapes every non-ASCII character.
func indentedJSON(value any) string {
	var out bytes.Buffer
	encodePy(&out, value, true, 2, 0)
	return out.String()
}

func encodePy(out *bytes.Buffer, value any, ensureASCII bool, indent, depth int) {
	newline := func(level int) {
		if indent >= 0 {
			out.WriteByte('\n')
			out.WriteString(strings.Repeat(" ", indent*level))
		}
	}
	itemSeparator, keySeparator := ",", ":"
	if indent >= 0 {
		keySeparator = ": "
	}
	switch v := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		out.WriteString(strconv.FormatBool(v))
	case string:
		encodePyString(out, v, ensureASCII)
	case int:
		out.WriteString(strconv.Itoa(v))
	case json.Number:
		if i, ok := jsonInt(v); ok {
			out.WriteString(i.String())
		} else {
			out.WriteString(pyFloatRepr(jsonFloat(v)))
		}
	case []any:
		if len(v) == 0 {
			out.WriteString("[]")
			return
		}
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteString(itemSeparator)
			}
			newline(depth + 1)
			encodePy(out, item, ensureASCII, indent, depth+1)
		}
		newline(depth)
		out.WriteByte(']')
	case map[string]any:
		if len(v) == 0 {
			out.WriteString("{}")
			return
		}
		keys := sortedKeys(v)
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteString(itemSeparator)
			}
			newline(depth + 1)
			encodePyString(out, key, ensureASCII)
			out.WriteString(keySeparator)
			encodePy(out, v[key], ensureASCII, indent, depth+1)
		}
		newline(depth)
		out.WriteByte('}')
	default:
		panic(fmt.Sprintf("unsupported JSON value %T", value))
	}
}

// sortedKeys orders keys like Python's sorted() on str: by code point, which
// for valid UTF-8 is byte order.
func sortedKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func encodePyString(out *bytes.Buffer, value string, ensureASCII bool) {
	out.WriteByte('"')
	for _, r := range value {
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
			switch {
			case r < 0x20 || (ensureASCII && r > 0x7e):
				if r > 0xffff {
					high, low := utf16Pair(r)
					fmt.Fprintf(out, `\u%04x\u%04x`, high, low)
				} else {
					fmt.Fprintf(out, `\u%04x`, r)
				}
			default:
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
}

func utf16Pair(r rune) (rune, rune) {
	r -= 0x10000
	return 0xd800 + (r>>10)&0x3ff, 0xdc00 + r&0x3ff
}

// pyFloatRepr is Python's float.__repr__ as json.dumps writes it.
func pyFloatRepr(value float64) string {
	switch {
	case math.IsNaN(value):
		return "NaN"
	case math.IsInf(value, 1):
		return "Infinity"
	case math.IsInf(value, -1):
		return "-Infinity"
	}
	scientific := strconv.FormatFloat(value, 'e', -1, 64)
	mantissa, exponentText, _ := strings.Cut(scientific, "e")
	exponent, _ := strconv.Atoi(exponentText)
	if exponent < -4 || exponent >= 16 {
		sign := "+"
		if exponent < 0 {
			sign, exponent = "-", -exponent
		}
		return fmt.Sprintf("%se%s%02d", mantissa, sign, exponent)
	}
	fixed := strconv.FormatFloat(value, 'f', -1, 64)
	if !strings.Contains(fixed, ".") {
		fixed += ".0"
	}
	return fixed
}

// pyRepr is Python's repr() of a json.loads value, as an f-string's !r writes it.
func pyRepr(value any) string {
	switch v := value.(type) {
	case nil:
		return "None"
	case bool:
		if v {
			return "True"
		}
		return "False"
	case string:
		return pyStringRepr(v)
	case json.Number:
		if i, ok := jsonInt(v); ok {
			return i.String()
		}
		f := jsonFloat(v)
		switch {
		case math.IsInf(f, 1):
			return "inf"
		case math.IsInf(f, -1):
			return "-inf"
		case math.IsNaN(f):
			return "nan"
		}
		return pyFloatRepr(f)
	case []any:
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = pyRepr(item)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		// json.loads keeps document order, which a Go map has lost; this
		// only affects the text of an unsafe-path refusal for an object.
		parts := make([]string, 0, len(v))
		for _, key := range sortedKeys(v) {
			parts = append(parts, pyStringRepr(key)+": "+pyRepr(v[key]))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
	return fmt.Sprint(value)
}

func pyStringRepr(value string) string {
	quote := byte('\'')
	if strings.Contains(value, "'") && !strings.Contains(value, `"`) {
		quote = '"'
	}
	var out strings.Builder
	out.WriteByte(quote)
	for _, r := range value {
		switch {
		case r == rune(quote) || r == '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case r == '\t':
			out.WriteString(`\t`)
		case r == '\n':
			out.WriteString(`\n`)
		case r == '\r':
			out.WriteString(`\r`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&out, `\x%02x`, r)
		case r < 0x7f || unicode.IsPrint(r):
			out.WriteRune(r)
		case r <= 0xff:
			fmt.Fprintf(&out, `\x%02x`, r)
		case r <= 0xffff:
			fmt.Fprintf(&out, `\u%04x`, r)
		default:
			fmt.Fprintf(&out, `\U%08x`, r)
		}
	}
	out.WriteByte(quote)
	return out.String()
}

// pyIsSpace is Python's str.isspace for one character: Unicode White_Space
// plus the ASCII information separators U+001C..U+001F.
func pyIsSpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

// pyStrip is Python's str.strip().
func pyStrip(value string) string {
	return strings.TrimFunc(value, pyIsSpace)
}

// pySplitlines is Python's str.splitlines(): it splits on every Unicode line
// boundary, treats \r\n as one, and drops a final empty line.
func pySplitlines(value string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(value); {
		r, size := utf8.DecodeRuneInString(value[i:])
		switch r {
		case '\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
			lines = append(lines, value[start:i])
			if r == '\r' && i+1 < len(value) && value[i+1] == '\n' {
				size++
			}
			start = i + size
		}
		i += size
	}
	if start < len(value) {
		lines = append(lines, value[start:])
	}
	return lines
}

var shlexUnsafe = regexp.MustCompile(`[^\w@%+=:,./-]`)

// shlexJoin is Python's shlex.join.
func shlexJoin(args ...string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		switch {
		case arg == "":
			quoted[i] = "''"
		case !shlexUnsafe.MatchString(arg):
			quoted[i] = arg
		default:
			quoted[i] = "'" + strings.ReplaceAll(arg, "'", `'"'"'`) + "'"
		}
	}
	return strings.Join(quoted, " ")
}
