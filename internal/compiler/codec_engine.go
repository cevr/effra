package compiler

import _ "embed"

// codecEngineJS is the JavaScript counterpart of runtime/effra's codec module:
// one reusable bounded JSON engine for profile effra/json-structural-1. A
// generated module appends it once when a selected codec plan needs it and
// supplies only plan literals; per-type output never copies the algorithm.
//
//go:embed codecs.mjs
var codecEngineJS string
