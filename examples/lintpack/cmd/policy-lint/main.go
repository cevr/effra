// Command policy-lint serves the example policy pack over the rule-pack
// process protocol. Its manifest names this program as the executable.
package main

import (
	"effra.local/prototype/examples/lintpack"
	"effra.local/prototype/lint"
)

func main() { lint.Main(lintpack.Pack) }
