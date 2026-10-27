package sketch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// KernelRootEnv names the environment variable that gives the kernel tests
// the repository root, the checkout whose runtime/rt kernel mode links. The
// gate sets it. Unset, the kernel tests skip and say so; set, any problem with
// the root fails them. The tests never guess the root from the directory
// layout. Prior art: Cargo gives tests CARGO_MANIFEST_DIR, and Go's own tests
// ask the go tool (internal/testenv).
const KernelRootEnv = "ERGO_REPO_ROOT"

// SketchModule is sketch9's own module. It is nested inside the repository,
// so a lookup that starts inside sketch9 passes over it to the module that
// holds runtime/rt.
const SketchModule = "effra.local/sketch9"

// KernelRoot checks that dir is a checkout kernel mode can link and returns
// its absolute path: a directory whose go.mod, as Go's own parser reads it,
// declares KernelModule, and which holds runtime/rt. Every problem is an error
// that names it: a missing directory, a go.mod Go cannot read (with the go
// tool's message), no module, another module, or no runtime/rt.
func KernelRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("repository root %s: %w", abs, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repository root %s is not a directory", abs)
	}
	mod, err := ModulePath(filepath.Join(abs, "go.mod"))
	if err != nil {
		return "", err
	}
	if mod != KernelModule {
		return "", fmt.Errorf("the checkout at %s declares module %s, but kernel mode links %s (sketch.KernelModule)", abs, mod, KernelModule)
	}
	if info, err := os.Stat(filepath.Join(abs, "runtime", "rt")); err != nil || !info.IsDir() {
		return "", fmt.Errorf("the checkout at %s declares module %s but has no runtime/rt directory", abs, mod)
	}
	return abs, nil
}

// ModulePath returns the module path that the go.mod file at gomod declares,
// read by the go tool (go mod edit -json), so Go's syntax decides: factored
// declarations and comments are read as Go reads them, and a file Go refuses
// is an error carrying the go tool's message.
func ModulePath(gomod string) (string, error) {
	gomod, err := filepath.Abs(gomod)
	if err != nil {
		return "", err
	}
	out, err := goTool(filepath.Dir(gomod), "mod", "edit", "-json", gomod)
	if err != nil {
		return "", err
	}
	var file struct{ Module *struct{ Path string } }
	if err := json.Unmarshal(out, &file); err != nil {
		return "", fmt.Errorf("go mod edit -json %s: %w", gomod, err)
	}
	if file.Module == nil || file.Module.Path == "" {
		return "", fmt.Errorf("%s declares no module", gomod)
	}
	return file.Module.Path, nil
}

// EnclosingModule returns the go.mod of the module that contains dir, as the
// go tool finds it (go env GOMOD), passing over sketch9's own module; found
// is false when no module contains dir. dir's symbolic links are resolved
// first, so a link that enters a checkout below its root still finds it.
func EnclosingModule(dir string) (gomod string, found bool, err error) {
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return "", false, err
	}
	for {
		out, err := goTool(dir, "env", "GOMOD")
		if err != nil {
			return "", false, err
		}
		gomod := strings.TrimSpace(string(out))
		if gomod == "" || gomod == os.DevNull {
			return "", false, nil
		}
		mod, err := ModulePath(gomod)
		if err != nil || mod != SketchModule {
			return gomod, true, err
		}
		parent := filepath.Dir(filepath.Dir(gomod))
		if parent == filepath.Dir(gomod) {
			return "", false, nil
		}
		dir = parent
	}
}

// goTool runs the go tool in dir and returns its standard output, or an error
// that carries its standard error.
func goTool(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("go %s: %s", strings.Join(args, " "), msg)
		}
		return nil, fmt.Errorf("go %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}
