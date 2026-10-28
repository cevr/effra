package report

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// FormatterSchemaVersion versions FormatResult and the `ergo fmt --json`
// report that carries its digests.
const FormatterSchemaVersion = 1

// FormatResult is the syntax-only formatting of one source text. Its digests
// are independent of any checked revision: formatting works on source that
// does not check.
type FormatResult struct {
	SchemaVersion int    `json:"schemaVersion"`
	InputDigest   string `json:"inputDigest"`
	OutputDigest  string `json:"outputDigest"`
	Changed       bool   `json:"changed"`
	Text          string `json:"text"`
}

// FormatFailure is a source the formatter cannot parse: its syntax
// diagnostics, and no replacement text.
type FormatFailure struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
}

func (e FormatFailure) Error() string {
	if len(e.Diagnostics) == 0 {
		return "source cannot be formatted"
	}
	return fmt.Sprintf("source cannot be formatted: %s", e.Diagnostics[0].Message)
}

// FormatLimitError means the complete formatted text would exceed the
// caller's output bound. It never carries partial text.
type FormatLimitError struct {
	Limit int
}

func (e FormatLimitError) Error() string {
	return fmt.Sprintf("formatted source exceeds %d-byte limit", e.Limit)
}

// FormatDigest is the hex SHA-256 of the exact source bytes, the digest
// FormatResult reports for its input and output.
func FormatDigest(source string) string {
	digest := sha256.Sum256([]byte(source))
	return hex.EncodeToString(digest[:])
}
