package effra

import (
	"embed"
	"strings"
)

//go:embed *.go
var sources embed.FS

// Sources bundles the same runtime source for standalone generated modules.
func Sources() map[string][]byte {
	entries, err := sources.ReadDir(".")
	if err != nil {
		panic(err)
	}
	out := map[string][]byte{}
	for _, entry := range entries {
		name := entry.Name()
		if name == "embed.go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := sources.ReadFile(name)
		if err != nil {
			panic(err)
		}
		out[name] = data
	}
	return out
}
