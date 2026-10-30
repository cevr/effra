package syntax

import (
	"errors"
	"strings"
	"testing"
)

// An unquoted path is one word. The grammar requires nonempty elements
// starting with a letter or underscore; it cannot turn a malformed word
// into a shorter path followed by a comment (#231).
func TestImportPathGrammar(t *testing.T) {
	for _, form := range []struct{ prefix, suffix string }{
		{"import ", "\n"},
		{"import go ", "\n"},
		{"import alias ", "\n"},
		{"import go alias ", "\n"},
		{"extern go ", ".F func F()\n"},
	} {
		for _, path := range []string{"0", "0//0", "a//b", "net//http", "net/http/", "net/.http", "net/h~ttp", "net/h+ttp", "a/*note*/"} {
			src := form.prefix + path + form.suffix
			_, err := Parse(src)
			var e *Error
			if !errors.As(err, &e) || e.Code != CodeSyntax || !strings.Contains(e.Message, "path") {
				t.Errorf("%q: got %v, want S001 for the malformed path", src, err)
			}
		}
		for _, path := range []string{"strings", "net/http", "example.com/json", "gopkg.in/yaml.v3", "_a/pkg-name", "α/β2"} {
			src := form.prefix + path + form.suffix
			if _, err := Parse(src); err != nil {
				t.Errorf("%q: %v", src, err)
			}
		}
	}
}
