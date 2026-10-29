package v1

import "embed"

// Source holds the runtime files that the lowering copies into every
// emitted module. This file is not among them: the copy needs no embed.
//
//go:embed runtime.go
var Source embed.FS

// SourceFiles names the files of Source, in the order they are copied.
var SourceFiles = []string{"runtime.go"}
