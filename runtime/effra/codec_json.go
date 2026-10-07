package effra

import "unicode/utf8"

type codecJSONKind uint8

const (
	codecJSONNull codecJSONKind = iota + 1
	codecJSONBool
	codecJSONNumber
	codecJSONString
	codecJSONArray
	codecJSONObject
)

// codecJSON is one admitted input value. Numbers are validated but not
// retained because CodecProfileJSON admits none outside ignored values.
type codecJSON struct {
	kind    codecJSONKind
	boolean bool
	text    string
	keys    []string
	values  []codecJSON
	index   map[string]int
}

// Objects index their keys once they outgrow a short linear scan, so
// duplicate detection and member lookup stay linear in the body size.
const codecLinearMembers = 8

func (v *codecJSON) member(key string) *codecJSON {
	if v.index != nil {
		if i, ok := v.index[key]; ok {
			return &v.values[i]
		}
		return nil
	}
	for i, candidate := range v.keys {
		if candidate == key {
			return &v.values[i]
		}
	}
	return nil
}

// addMember reports false when key is already present.
func (v *codecJSON) addMember(key string, value codecJSON) bool {
	if v.member(key) != nil {
		return false
	}
	v.keys = append(v.keys, key)
	v.values = append(v.values, value)
	if v.index != nil {
		v.index[key] = len(v.keys) - 1
	} else if len(v.keys) > codecLinearMembers {
		v.index = make(map[string]int, len(v.keys)*2)
		for i, candidate := range v.keys {
			v.index[candidate] = i
		}
	}
	return true
}

// codecParser is the profile's single admission pass: RFC 8259 grammar with
// no byte-order mark, well-formed UTF-8, paired surrogate escapes, unique
// object keys and bounded nesting. Every failure reports the byte offset
// where admission stopped; the JavaScript engine reports identical offsets.
type codecParser struct {
	data     []byte
	pos      int
	depth    int
	maxDepth int
}

func (p *codecParser) fail(reason CodecReason, offset int) *CodecError {
	return &CodecError{Direction: CodecDecode, Reason: reason, Path: []string{}, Offset: offset}
}

func (p *codecParser) document() (codecJSON, *CodecError) {
	p.space()
	value, failure := p.value()
	if failure != nil {
		return codecJSON{}, failure
	}
	p.space()
	if p.pos != len(p.data) {
		return codecJSON{}, p.fail(CodecSyntax, p.pos)
	}
	return value, nil
}

func (p *codecParser) space() {
	for p.pos < len(p.data) {
		switch p.data[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *codecParser) at(c byte) bool {
	return p.pos < len(p.data) && p.data[p.pos] == c
}

func (p *codecParser) digit() bool {
	return p.pos < len(p.data) && p.data[p.pos] >= '0' && p.data[p.pos] <= '9'
}

func (p *codecParser) value() (codecJSON, *CodecError) {
	if p.pos >= len(p.data) {
		return codecJSON{}, p.fail(CodecSyntax, p.pos)
	}
	switch c := p.data[p.pos]; {
	case c == '{':
		return p.object()
	case c == '[':
		return p.array()
	case c == '"':
		text, failure := p.string()
		return codecJSON{kind: codecJSONString, text: text}, failure
	case c == 't':
		return codecJSON{kind: codecJSONBool, boolean: true}, p.literal("true")
	case c == 'f':
		return codecJSON{kind: codecJSONBool}, p.literal("false")
	case c == 'n':
		return codecJSON{kind: codecJSONNull}, p.literal("null")
	case c == '-' || c >= '0' && c <= '9':
		return codecJSON{kind: codecJSONNumber}, p.number()
	}
	return codecJSON{}, p.fail(CodecSyntax, p.pos)
}

func (p *codecParser) enter() *CodecError {
	p.depth++
	if p.depth > p.maxDepth {
		return p.fail(CodecDepth, p.pos)
	}
	p.pos++
	p.space()
	return nil
}

func (p *codecParser) object() (codecJSON, *CodecError) {
	object := codecJSON{kind: codecJSONObject}
	if failure := p.enter(); failure != nil {
		return object, failure
	}
	if p.at('}') {
		p.pos++
		p.depth--
		return object, nil
	}
	for {
		if !p.at('"') {
			return object, p.fail(CodecSyntax, p.pos)
		}
		keyOffset := p.pos
		key, failure := p.string()
		if failure != nil {
			return object, failure
		}
		if object.member(key) != nil {
			return object, p.fail(CodecDuplicateKey, keyOffset)
		}
		p.space()
		if !p.at(':') {
			return object, p.fail(CodecSyntax, p.pos)
		}
		p.pos++
		p.space()
		member, failure := p.value()
		if failure != nil {
			return object, failure
		}
		object.addMember(key, member)
		p.space()
		switch {
		case p.at(','):
			p.pos++
			p.space()
		case p.at('}'):
			p.pos++
			p.depth--
			return object, nil
		default:
			return object, p.fail(CodecSyntax, p.pos)
		}
	}
}

func (p *codecParser) array() (codecJSON, *CodecError) {
	array := codecJSON{kind: codecJSONArray}
	if failure := p.enter(); failure != nil {
		return array, failure
	}
	if p.at(']') {
		p.pos++
		p.depth--
		return array, nil
	}
	for {
		item, failure := p.value()
		if failure != nil {
			return array, failure
		}
		array.values = append(array.values, item)
		p.space()
		switch {
		case p.at(','):
			p.pos++
			p.space()
		case p.at(']'):
			p.pos++
			p.depth--
			return array, nil
		default:
			return array, p.fail(CodecSyntax, p.pos)
		}
	}
}

func (p *codecParser) literal(word string) *CodecError {
	for i := 0; i < len(word); i++ {
		if p.pos+i >= len(p.data) || p.data[p.pos+i] != word[i] {
			return p.fail(CodecSyntax, p.pos+i)
		}
	}
	p.pos += len(word)
	return nil
}

func (p *codecParser) number() *CodecError {
	if p.at('-') {
		p.pos++
	}
	if !p.digit() {
		return p.fail(CodecSyntax, p.pos)
	}
	if p.at('0') {
		p.pos++
	} else {
		for p.digit() {
			p.pos++
		}
	}
	if p.at('.') {
		p.pos++
		if !p.digit() {
			return p.fail(CodecSyntax, p.pos)
		}
		for p.digit() {
			p.pos++
		}
	}
	if p.at('e') || p.at('E') {
		p.pos++
		if p.at('+') || p.at('-') {
			p.pos++
		}
		if !p.digit() {
			return p.fail(CodecSyntax, p.pos)
		}
		for p.digit() {
			p.pos++
		}
	}
	return nil
}

// string decodes one quoted string. A malformed escape fails at its
// backslash; an unpaired surrogate escape or ill-formed UTF-8 sequence fails
// as invalid Unicode at its first byte.
func (p *codecParser) string() (string, *CodecError) {
	p.pos++
	start := p.pos
	var decoded []byte
	for {
		if p.pos >= len(p.data) {
			return "", p.fail(CodecSyntax, p.pos)
		}
		c := p.data[p.pos]
		switch {
		case c == '"':
			text := ""
			if decoded == nil {
				text = string(p.data[start:p.pos])
			} else {
				text = string(append(decoded, p.data[start:p.pos]...))
			}
			p.pos++
			return text, nil
		case c == '\\':
			decoded = append(decoded, p.data[start:p.pos]...)
			var failure *CodecError
			if decoded, failure = p.escape(decoded); failure != nil {
				return "", failure
			}
			start = p.pos
		case c < 0x20:
			return "", p.fail(CodecSyntax, p.pos)
		case c < utf8.RuneSelf:
			p.pos++
		default:
			r, size := utf8.DecodeRune(p.data[p.pos:])
			if r == utf8.RuneError && size == 1 {
				return "", p.fail(CodecInvalidUnicode, p.pos)
			}
			p.pos += size
		}
	}
}

func (p *codecParser) escape(decoded []byte) ([]byte, *CodecError) {
	offset := p.pos
	if p.pos+1 >= len(p.data) {
		return decoded, p.fail(CodecSyntax, offset)
	}
	simple := byte(0)
	switch p.data[p.pos+1] {
	case '"':
		simple = '"'
	case '\\':
		simple = '\\'
	case '/':
		simple = '/'
	case 'b':
		simple = '\b'
	case 'f':
		simple = '\f'
	case 'n':
		simple = '\n'
	case 'r':
		simple = '\r'
	case 't':
		simple = '\t'
	case 'u':
		unit, ok := p.unicodeEscape(p.pos)
		if !ok {
			return decoded, p.fail(CodecSyntax, offset)
		}
		p.pos += 6
		r := rune(unit)
		switch {
		case unit >= 0xdc00 && unit <= 0xdfff:
			return decoded, p.fail(CodecInvalidUnicode, offset)
		case unit >= 0xd800 && unit <= 0xdbff:
			low, ok := p.unicodeEscape(p.pos)
			if !ok || low < 0xdc00 || low > 0xdfff {
				return decoded, p.fail(CodecInvalidUnicode, offset)
			}
			p.pos += 6
			r = 0x10000 + (rune(unit)-0xd800)<<10 + (rune(low) - 0xdc00)
		}
		return utf8.AppendRune(decoded, r), nil
	default:
		return decoded, p.fail(CodecSyntax, offset)
	}
	p.pos += 2
	return append(decoded, simple), nil
}

// unicodeEscape reads a complete \uXXXX escape at offset.
func (p *codecParser) unicodeEscape(offset int) (uint16, bool) {
	if offset+6 > len(p.data) || p.data[offset] != '\\' || p.data[offset+1] != 'u' {
		return 0, false
	}
	unit := uint16(0)
	for _, c := range p.data[offset+2 : offset+6] {
		switch {
		case c >= '0' && c <= '9':
			c -= '0'
		case c >= 'a' && c <= 'f':
			c -= 'a' - 10
		case c >= 'A' && c <= 'F':
			c -= 'A' - 10
		default:
			return 0, false
		}
		unit = unit<<4 | uint16(c)
	}
	return unit, true
}

// writeString writes valid UTF-8 text as a JSON string with the escaping of
// ECMAScript JSON.stringify: quote, backslash and C0 controls only, short
// escapes where defined and lowercase \u00XX otherwise. The remaining
// allowance is checked before every append, so a string whose encoding does
// not fit fails without constructing any output beyond the bound.
func (e *codecEncoder) writeString(text string) *CodecError {
	const hex = "0123456789abcdef"
	if failure := e.write('"'); failure != nil {
		return failure
	}
	start := 0
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c >= 0x20 && c != '"' && c != '\\' {
			if len(e.out)+i+1-start > e.limit {
				return codecFailure(CodecEncode, CodecBodyTooLarge, nil)
			}
			continue
		}
		e.out = append(e.out, text[start:i]...)
		start = i + 1
		var failure *CodecError
		switch c {
		case '"', '\\':
			failure = e.write('\\', c)
		case '\b':
			failure = e.write('\\', 'b')
		case '\f':
			failure = e.write('\\', 'f')
		case '\n':
			failure = e.write('\\', 'n')
		case '\r':
			failure = e.write('\\', 'r')
		case '\t':
			failure = e.write('\\', 't')
		default:
			failure = e.write('\\', 'u', '0', '0', hex[c>>4], hex[c&0xf])
		}
		if failure != nil {
			return failure
		}
	}
	e.out = append(e.out, text[start:]...)
	return e.write('"')
}
