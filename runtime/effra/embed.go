package effra

import (
	"embed"
	"fmt"
	"sort"
)

//go:embed effect.go fiber.go managed.go scheduler.go scope.go layers.go latch.go http.go files.go interop.go console.go env.go inspect.go
var sources embed.FS

// RuntimeModule identifies one selectable group of native runtime sources.
type RuntimeModule string

const (
	RuntimeModuleCore    RuntimeModule = "core"
	RuntimeModuleLayers  RuntimeModule = "layers"
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
}

// runtimeModuleCatalog is immutable by convention: callers receive only
// copied source maps and byte slices, never this catalog or its file lists.
var runtimeModuleCatalog = map[RuntimeModule]runtimeModuleSpec{
	RuntimeModuleCore: {
		files: []string{"effect.go", "fiber.go", "managed.go", "scheduler.go", "scope.go"},
	},
	RuntimeModuleLayers: {
		files:        []string{"layers.go"},
		dependencies: []RuntimeModule{RuntimeModuleCore},
	},
	RuntimeModuleSync: {
		files:        []string{"latch.go"},
		dependencies: []RuntimeModule{RuntimeModuleCore},
	},
	RuntimeModuleHTTP: {
		files:        []string{"http.go"},
		dependencies: []RuntimeModule{RuntimeModuleCore},
	},
	RuntimeModuleFiles: {
		files:        []string{"files.go"},
		dependencies: []RuntimeModule{RuntimeModuleCore},
	},
	RuntimeModuleConsole: {
		files:        []string{"console.go"},
		dependencies: []RuntimeModule{RuntimeModuleCore},
	},
	RuntimeModuleEnv: {
		files:        []string{"env.go"},
		dependencies: []RuntimeModule{RuntimeModuleCore},
	},
	RuntimeModuleInspect: {
		files:        []string{"inspect.go"},
		dependencies: []RuntimeModule{RuntimeModuleCore},
	},
	RuntimeModuleInterop: {
		files:        []string{"interop.go"},
		dependencies: []RuntimeModule{RuntimeModuleCore},
	},
}

func catalogSourceFiles() []string {
	files := map[string]struct{}{}
	for _, spec := range runtimeModuleCatalog {
		for _, name := range spec.files {
			files[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func readRuntimeSource(name string) []byte {
	data, err := sources.ReadFile(name)
	if err != nil {
		panic(err)
	}
	return data
}

// Sources bundles the same runtime source for standalone generated modules.
func Sources() map[string][]byte {
	names := catalogSourceFiles()
	out := make(map[string][]byte, len(names))
	for _, name := range names {
		out[name] = readRuntimeSource(name)
	}
	return out
}

// SelectModules closes roots over the catalog's declared dependencies and
// returns the selected modules in identity order. The catalog is the only
// dependency authority: callers name roots, never a module's dependencies.
func SelectModules(roots ...RuntimeModule) ([]RuntimeModule, error) {
	selected := map[RuntimeModule]struct{}{}
	visiting := map[RuntimeModule]struct{}{}
	var visit func(RuntimeModule) error
	visit = func(module RuntimeModule) error {
		if _, ok := selected[module]; ok {
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
		selected[module] = struct{}{}
		return nil
	}
	for _, root := range roots {
		if err := visit(root); err != nil {
			return nil, err
		}
	}
	modules := make([]RuntimeModule, 0, len(selected))
	for module := range selected {
		modules = append(modules, module)
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i] < modules[j] })
	return modules, nil
}

// SelectSources returns a fresh source snapshot for the transitive closure of
// roots. An empty root set selects no files; it never means all runtime files.
func SelectSources(roots ...RuntimeModule) (map[string][]byte, error) {
	modules, err := SelectModules(roots...)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for _, module := range modules {
		for _, name := range runtimeModuleCatalog[module].files {
			out[name] = readRuntimeSource(name)
		}
	}
	return out, nil
}
