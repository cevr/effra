package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
)

const MaxFrameBytes = 2 << 20
const MaxHeaderBytes = 8 << 10
const MaxOutputBytes = 2 << 20

// A framing error is terminal: after an invalid length there is no safe
// boundary at which to resume the byte stream. Invalid JSON bodies are recoverable.
func readFrame(r *bufio.Reader) ([]byte, error) {
	length, total := -1, 0
	for {
		line, err := r.ReadSlice('\n')
		if err == io.EOF && total == 0 && len(line) == 0 {
			return nil, io.EOF
		}
		if err != nil {
			return nil, fmt.Errorf("incomplete or oversized LSP header: %w", err)
		}
		total += len(line)
		if total > MaxHeaderBytes {
			return nil, fmt.Errorf("LSP headers exceed %d bytes", MaxHeaderBytes)
		}
		if !bytes.HasSuffix(line, []byte("\r\n")) {
			return nil, fmt.Errorf("LSP headers require CRLF")
		}
		for _, b := range line {
			if b > 127 {
				return nil, fmt.Errorf("LSP headers must be ASCII")
			}
		}
		if len(line) == 2 {
			break
		}
		name, value, ok := strings.Cut(string(line[:len(line)-2]), ":")
		if !ok || name == "" || strings.ContainsAny(name, " \t") {
			return nil, fmt.Errorf("malformed LSP header")
		}
		switch strings.ToLower(name) {
		case "content-length":
			value = strings.TrimSpace(value)
			if length != -1 || value == "" || strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
				return nil, fmt.Errorf("invalid Content-Length")
			}
			n, err := strconv.ParseUint(value, 10, 31)
			if err != nil || n == 0 || n > MaxFrameBytes {
				return nil, fmt.Errorf("Content-Length must be 1..%d", MaxFrameBytes)
			}
			length = int(n)
		case "content-type":
			value = strings.ToLower(strings.TrimSpace(value))
			if value != "application/vscode-jsonrpc; charset=utf-8" && value != "application/vscode-jsonrpc; charset=utf8" && value != "application/vscode-jsonrpc" {
				return nil, fmt.Errorf("unsupported Content-Type")
			}
		default: // Extension headers have no effect on framing.
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("incomplete LSP body: %w", err)
	}
	return body, nil
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > MaxOutputBytes-b.Len() {
		return 0, fmt.Errorf("LSP output exceeds %d bytes", MaxOutputBytes)
	}
	return b.Buffer.Write(p)
}

func encodeFrame(value any) ([]byte, error) {
	// Charge a conservative JSON byte bound before encoding/json allocates its
	// internal serialization buffer. All outgoing values are owned acyclic data.
	budget := MaxOutputBytes
	if !chargeJSON(reflect.ValueOf(value), &budget) {
		return nil, fmt.Errorf("LSP output exceeds %d byte budget", MaxOutputBytes)
	}
	var body boundedBuffer
	if err := json.NewEncoder(&body).Encode(value); err != nil {
		return nil, err
	}
	header := []byte(fmt.Sprintf("Content-Length: %d\r\n\r\n", body.Len()))
	return append(header, body.Bytes()...), nil
}

func chargeJSON(v reflect.Value, budget *int) bool {
	*budget -= 32 // Container punctuation, field names and scalar representations.
	if *budget < 0 {
		return false
	}
	if !v.IsValid() {
		return true
	}
	switch v.Kind() {
	case reflect.Interface, reflect.Pointer:
		if !v.IsNil() {
			return chargeJSON(v.Elem(), budget)
		}
	case reflect.String:
		if v.Len() > *budget/6 {
			return false
		}
		*budget -= v.Len() * 6
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			if !chargeJSON(iter.Key(), budget) || !chargeJSON(iter.Value(), budget) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if !chargeJSON(v.Index(i), budget) {
				return false
			}
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !chargeJSON(reflect.ValueOf(v.Type().Field(i).Name), budget) || !chargeJSON(v.Field(i), budget) {
				return false
			}
		}
	}
	return *budget >= 0
}

func writeFrame(w io.Writer, frame []byte) error {
	for len(frame) > 0 {
		n, err := w.Write(frame)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		frame = frame[n:]
	}
	return nil
}

// encoding/json replaces unpaired UTF-16 escapes with U+FFFD. Refuse that
// lossy input rather than attach diagnostics to text the client did not send.
func validEscapes(body []byte) bool {
	for i := 0; i < len(body); i++ {
		if body[i] != '\\' {
			continue
		}
		i++
		if i >= len(body) {
			return false
		}
		if body[i] != 'u' {
			continue
		}
		if i+4 >= len(body) {
			return false
		}
		n, err := strconv.ParseUint(string(body[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		if n >= 0xd800 && n <= 0xdbff {
			if i+6 >= len(body) || body[i+1] != '\\' || body[i+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(body[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return true
}
