// Command policy-lint serves the example policy pack over the rule-pack
// process protocol. Its manifest names this program as the executable.
package main

import (
	"ergo.local/prototype/examples/lintpack"
	"ergo.local/prototype/lint"
)

func main() { lint.Main(lintpack.Pack) }
