// Package edition resolves the language edition that governs a source file
// and declares the diagnostics of that resolution. The CLI, MCP and LSP open
// a source file once, bind the open file to its edition with ResolveFile,
// read the bytes from that open file, and route them to the front-end of the
// resolved edition. The edition comes only from an edition manifest, never
// from the file's content.
//
// The package is a leaf: it imports neither the compiler nor a surface, so
// every surface and every compiler test can use it.
package edition

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Edition identifies one language edition. Ids are exact and case-sensitive.
type Edition string

const (
	// Legacy is the earlier .ef language that internal/compiler implements.
	Legacy Edition = "legacy"
	// E2026 is the Go-shaped language of docs/spec/ v0.
	E2026 Edition = "2026"
)

// Supported lists every edition id a manifest may name, in message order.
var Supported = []Edition{Legacy, E2026}

// Default is the edition of a file that no manifest governs.
const Default = Legacy

// ManifestName is the file name of an edition manifest.
const ManifestName = "ergo.json"

// MaxManifestBytes bounds the manifest a resolution reads.
const MaxManifestBytes = 64 << 10

// The sources of a resolution.
const (
	SourceDefault  = "default"
	SourceManifest = "manifest"
	// SourceAdmission: the document could not be admitted (EF146), so no
	// manifest was consulted. Only ResolveDocument returns it.
	SourceAdmission = "admission"
)

// The diagnostic codes this package declares. docs/tooling.md documents each
// one in its edition table, and a test keeps the two in step.
const (
	// CodeUnsupported: the edition manifest is unsupported or malformed.
	CodeUnsupported = "EF144"
	// CodeUnavailable: the operation is not available for this edition yet.
	CodeUnavailable = "EF145"
	// CodeAdmission: the source could not be bound to an edition: it changed
	// between open and edition lookup, or its path exists but is not a
	// regular file that can be opened.
	CodeAdmission = "EF146"
)

// Codes lists every diagnostic code this package declares.
var Codes = []string{CodeUnsupported, CodeUnavailable, CodeAdmission}

// The tickets that add an operation the 2026 edition lacks today.
const (
	TicketChecker = "#196"
	TicketTests   = "#204"
)

// Diagnostic is an edition refusal. It has no source location: the refusal
// happens before any front-end reads the file.
type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// String is the CODE: message form every surface prints.
func (d Diagnostic) String() string { return d.Code + ": " + d.Message }

// Resolution is the edition that governs one directory and where it came
// from. On EF144, Edition is empty and Diagnostic is set.
type Resolution struct {
	Edition    Edition
	Source     string
	Manifest   string
	Diagnostic *Diagnostic
}

// Resolve returns the edition that governs a source file in dir. It walks
// from dir upward. At each directory, an edition manifest decides. The walk
// ends after the first directory that holds a go.mod (a manifest never
// governs across a Go module boundary), or at the filesystem root. With no
// manifest, the edition is Default with source "default". An entry on the walk that
// cannot be inspected (any error but not-exist) is never taken as missing:
// the resolution is the EF146 refusal with source "admission". Resolve never
// reads a source file and keeps no cache, so a manifest edit takes effect at
// the next call. Surfaces do not call it: they admit a source with ResolveFile or
// ResolveDocument, which walk the same rule from a physical path and
// validate what they read.
func Resolve(dir string) Resolution {
	walked := walk(dir)
	walked.close()
	return walked.resolution
}

// manifestResolution decides the edition from a manifest's bytes: exactly
// one JSON object {"edition": "<id>"} naming a supported edition.
func manifestResolution(path string, data []byte) Resolution {
	value, reason := parseManifest(data)
	if reason != "" {
		return refuse(path, reason)
	}
	var id string
	if json.Unmarshal(value, &id) != nil {
		return refuse(path, fmt.Sprintf("names edition %s, which is not a string", clip(string(value))))
	}
	for _, supported := range Supported {
		if Edition(id) == supported {
			return Resolution{Edition: supported, Source: SourceManifest, Manifest: path}
		}
	}
	return refuse(path, fmt.Sprintf("names unsupported edition %s", clip(string(value))))
}

// parseManifest returns the raw value of the one edition field, or the
// reason the manifest is malformed. Unknown and repeated fields, a
// non-object and trailing data are all malformed.
func parseManifest(data []byte) (json.RawMessage, string) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return nil, "is not valid JSON"
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return nil, "is not a JSON object"
	}
	var value json.RawMessage
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, "is not valid JSON"
		}
		key, _ := token.(string)
		if key != "edition" {
			return nil, fmt.Sprintf("has unknown field %s", clip(fmt.Sprintf("%q", key)))
		}
		if value != nil {
			return nil, `repeats the field "edition"`
		}
		if err := decoder.Decode(&value); err != nil {
			return nil, "is not valid JSON"
		}
	}
	if _, err := decoder.Token(); err != nil {
		return nil, "is not valid JSON"
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, "has data after the JSON object"
	}
	if value == nil {
		return nil, `has no "edition" field`
	}
	return value, ""
}

func refuse(path, reason string) Resolution {
	return Resolution{Source: SourceManifest, Manifest: path, Diagnostic: &Diagnostic{
		Code:    CodeUnsupported,
		Message: fmt.Sprintf("edition manifest %s %s; it must be one JSON object {\"edition\": \"ID\"} naming a supported edition: %s", path, reason, supportedList()),
	}}
}

func supportedList() string {
	quoted := make([]string, len(Supported))
	for i, id := range Supported {
		quoted[i] = fmt.Sprintf("%q", string(id))
	}
	return strings.Join(quoted, ", ")
}

// clip bounds a manifest value quoted in a message.
func clip(text string) string {
	const limit = 64
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && text[cut]&0xC0 == 0x80 {
		cut--
	}
	return text[:cut] + "..."
}

// JSON is the canonical projection of the resolution: compact JSON with the
// keys edition, source, manifest and, on EF144 only, diagnostic. Every
// surface returns these bytes unchanged. HTML characters are escaped as
// encoding/json escapes them, so a surface that compacts the bytes again
// leaves them identical.
func (r Resolution) JSON() []byte {
	var edition any
	if r.Diagnostic == nil {
		edition = string(r.Edition)
	}
	projection := struct {
		Edition    any         `json:"edition"`
		Source     string      `json:"source"`
		Manifest   string      `json:"manifest"`
		Diagnostic *Diagnostic `json:"diagnostic,omitempty"`
	}{edition, r.Source, r.Manifest, r.Diagnostic}
	data, err := json.Marshal(projection)
	if err != nil {
		panic(err) // strings and a pointer to strings always encode
	}
	return data
}

// Unavailable is the EF145 refusal of an operation that the edition does
// not support yet; ticket names the work that adds it.
func Unavailable(operation string, edition Edition, ticket string) Diagnostic {
	return Diagnostic{Code: CodeUnavailable, Message: fmt.Sprintf("%s is not available for edition %q yet; %s adds it", operation, string(edition), ticket)}
}
