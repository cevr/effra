package v1

import runtimefiles "ergo.local/prototype/runtime"

// Source holds this package's runtime files as the one embed owner,
// runtime/embed.go, holds them. This file is not among them: it serves
// only the compiler.
var Source = runtimefiles.Package("v1")

// SourceFiles names the files of Source that the lowering copies today, in
// order. runtime.go alone needs no other runtime package. When the lowering
// lowers the std packages task, scope and queue (lower-v1 stage 3b, after
// check-v1 stage D), it copies runtimefiles.Files("runtime/v1") instead:
// every file of this package and of runtime/rt, which it imports.
var SourceFiles = []string{"runtime.go"}
