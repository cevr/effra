package sketch

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Checkout is a repository checkout that can hold the kernel runtime.
type Checkout struct {
	// Root is the checkout's root directory.
	Root string
	// Module is the module its go.mod declares, which may differ from
	// KernelModule: the root module was renamed once (effra.local to
	// ergo.local, #218), and a caller that links KernelModule must refuse a
	// mismatch rather than look further up or pass over it.
	Module string
}

// FindCheckout finds the repository checkout nearest at or above dir. A
// directory is a checkout by its structure, when it holds runtime/rt, whether
// or not its go.mod reads; or when its go.mod names a module of the root
// module's shape, "<name>.local/prototype". found is false only when no
// directory above qualifies, as for a copy of sketch9 outside the repository.
// Once a checkout is found, a go.mod that is missing, unreadable or declares
// no module is an error that names the problem, never an absence.
func FindCheckout(dir string) (c Checkout, found bool, err error) {
	dir, err = filepath.Abs(dir)
	if err != nil {
		return Checkout{}, false, err
	}
	for {
		gomod := filepath.Join(dir, "go.mod")
		rt, err := isDir(filepath.Join(dir, "runtime", "rt"))
		if err != nil {
			return Checkout{Root: dir}, true, err
		}
		if rt {
			mod, err := goModModule(gomod)
			if err != nil {
				return Checkout{Root: dir}, true, fmt.Errorf("checkout at %s (it holds runtime/rt): %w", dir, err)
			}
			return Checkout{Root: dir, Module: mod}, true, nil
		}
		// Without runtime/rt a go.mod marks a checkout only by its module's
		// shape. A go.mod that does not read here, such as sketch9's own when
		// it is broken, is not evidence of a checkout.
		if mod, err := goModModule(gomod); err == nil && isPrototypeModule(mod) {
			return Checkout{Root: dir, Module: mod}, true, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Checkout{}, false, nil
		}
		dir = parent
	}
}

// isPrototypeModule reports whether mod has the root module's shape,
// "<name>.local/prototype".
func isPrototypeModule(mod string) bool {
	host := path.Dir(mod)
	return path.Base(mod) == "prototype" && strings.HasSuffix(host, ".local") && !strings.Contains(host, "/")
}

// goModModule returns the module path that the go.mod file at gomod declares.
// It reads the whole file, so no line length limits it, and it is an error
// when the file is missing or unreadable or declares no module.
func goModModule(gomod string) (string, error) {
	raw, err := os.ReadFile(gomod)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("no go.mod at %s", gomod)
	}
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", gomod, err)
	}
	for i, line := range strings.Split(string(raw), "\n") {
		line, _, _ = strings.Cut(line, "//")
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "module" {
			continue
		}
		if len(fields) != 2 || fields[1] == "(" {
			return "", fmt.Errorf("%s:%d: cannot read the module line %q", gomod, i+1, strings.TrimSpace(line))
		}
		mod := fields[1]
		if unq, err := strconv.Unquote(mod); err == nil {
			mod = unq
		}
		return mod, nil
	}
	return "", fmt.Errorf("%s declares no module", gomod)
}

// isDir reports whether p is a directory. A p that does not exist, or whose
// parent is a file, is not one; any other failure to inspect it is an error,
// since it may hide a checkout.
func isDir(p string) (bool, error) {
	info, err := os.Stat(p)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspecting %s: %w", p, err)
	}
	return info.IsDir(), nil
}
