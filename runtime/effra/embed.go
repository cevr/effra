package effra

import (
	"embed"
	"fmt"
)

//go:embed effect.go fiber.go managed.go scheduler.go scope.go latch.go http.go files.go interop.go console.go env.go inspect.go
var sources embed.FS

// RuntimeModule identifies one selectable group of native runtime sources.
type RuntimeModule string

const (
	RuntimeModuleCore    RuntimeModule = "core"
	RuntimeModuleSync    RuntimeModule = "sync"
	RuntimeModuleHTTP    RuntimeModule = "http"
	RuntimeModuleFiles   RuntimeModule = "files"
	RuntimeModuleConsole RuntimeModule = "console"
	RuntimeModuleEnv     RuntimeModule = "env"
	RuntimeModuleInspect RuntimeModule = "inspect"
	RuntimeModuleInterop RuntimeModule = "interop"
)

type runtimeModuleSpec struct {
	files        []string
	dependencies []RuntimeModule
	imports      []string
}

// runtimeSourceFiles is the authoritative list of runtime files admitted to
// generated applications. The embed directive above mirrors it because Go's
// embed patterns must be literal; tests verify that the two lists agree.
var runtimeSourceFiles = [...]string{
	"effect.go",
	"fiber.go",
	"managed.go",
	"scheduler.go",
	"scope.go",
	"latch.go",
	"http.go",
	"files.go",
	"interop.go",
	"console.go",
	"env.go",
	"inspect.go",
}

// runtimeModuleCatalog is immutable by convention: callers receive only
// copied source maps and byte slices, never this catalog or its file lists.
var runtimeModuleCatalog = map[RuntimeModule]runtimeModuleSpec{
	RuntimeModuleCore: {
		files: []string{"effect.go", "fiber.go", "managed.go", "scheduler.go", "scope.go"},
		imports: []string{
			"context", "errors", "fmt", "math", "sort", "strings", "sync", "sync/atomic", "time",
		},
	},
	RuntimeModuleSync: {
		files:        []string{"latch.go"},
		dependencies: []RuntimeModule{RuntimeModuleCore},
		imports:      []string{"context", "errors"},
	},
	RuntimeModuleHTTP: {
		files:        []string{"http.go"},
		dependencies: []RuntimeModule{RuntimeModuleCore},
		imports:      []string{"context", "errors", "net", "net/http", "time"},
	},
	RuntimeModuleFiles: {
		files:        []string{"files.go"},
		dependencies: []RuntimeModule{RuntimeModuleCore},
		imports:      []string{"context", "fmt", "io", "os", "sync"},
	},
	RuntimeModuleConsole: {
		files:        []string{"console.go"},
		dependencies: []RuntimeModule{RuntimeModuleCore},
		imports:      []string{"fmt"},
	},
	RuntimeModuleEnv: {
		files:        []string{"env.go"},
		dependencies: []RuntimeModule{RuntimeModuleCore},
		imports:      []string{"os"},
	},
	RuntimeModuleInspect: {
		files:        []string{"inspect.go"},
		dependencies: []RuntimeModule{RuntimeModuleCore},
		imports:      []string{"encoding/json"},
	},
	RuntimeModuleInterop: {
		files:        []string{"interop.go"},
		dependencies: []RuntimeModule{RuntimeModuleCore},
		imports:      []string{"context"},
	},
}

func readRuntimeSource(name string) []byte {
	data, err := sources.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return append([]byte(nil), data...)
}

// Sources bundles the same runtime source for standalone generated modules.
func Sources() map[string][]byte {
	out := make(map[string][]byte, len(runtimeSourceFiles))
	for _, name := range runtimeSourceFiles {
		out[name] = readRuntimeSource(name)
	}
	return out
}

// SelectSources returns a fresh source snapshot for the transitive closure of
// roots. An empty root set selects no files; it never means all runtime files.
func SelectSources(roots ...RuntimeModule) (map[string][]byte, error) {
	selectedModules := map[RuntimeModule]struct{}{}
	selectedFiles := map[string]struct{}{}
	visiting := map[RuntimeModule]struct{}{}
	var visit func(RuntimeModule) error
	visit = func(module RuntimeModule) error {
		if _, ok := selectedModules[module]; ok {
			return nil
		}
		spec, ok := runtimeModuleCatalog[module]
		if !ok {
			return fmt.Errorf("unknown runtime module %q", module)
		}
		if _, ok := visiting[module]; ok {
			return fmt.Errorf("runtime module dependency cycle at %q", module)
		}
		visiting[module] = struct{}{}
		for _, dependency := range spec.dependencies {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		delete(visiting, module)
		selectedModules[module] = struct{}{}
		for _, name := range spec.files {
			selectedFiles[name] = struct{}{}
		}
		return nil
	}
	for _, root := range roots {
		if err := visit(root); err != nil {
			return nil, err
		}
	}
	out := make(map[string][]byte, len(selectedFiles))
	for _, name := range runtimeSourceFiles {
		if _, ok := selectedFiles[name]; ok {
			out[name] = readRuntimeSource(name)
		}
	}
	return out, nil
}
