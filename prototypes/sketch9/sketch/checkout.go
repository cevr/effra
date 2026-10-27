package sketch

import (
	"bufio"
	"bytes"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// FindCheckout finds the repository checkout nearest at or above dir: the
// first directory whose go.mod names a module "<name>.local/prototype", or
// whose go.mod sits next to runtime/rt. It returns the directory and the
// module its go.mod declares, which may differ from KernelModule: the root
// module was renamed once (effra.local to ergo.local, #218), and a caller
// that links KernelModule must refuse a mismatch rather than look further up
// or pass over it. ok is false when no directory above qualifies, as for a
// copy of sketch9 outside the repository.
func FindCheckout(dir string) (root, module string, ok bool) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", "", false
	}
	for {
		if mod, has := goModModule(filepath.Join(dir, "go.mod")); has && (isPrototypeModule(mod) || isDir(filepath.Join(dir, "runtime", "rt"))) {
			return dir, mod, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", "", false
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

// goModModule returns the module path that the go.mod file at gomod declares;
// has is false when the file is missing or declares none.
func goModModule(gomod string) (string, bool) {
	raw, err := os.ReadFile(gomod)
	if err != nil {
		return "", false
	}
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || fields[0] != "module" {
			continue
		}
		mod := fields[1]
		if unq, err := strconv.Unquote(mod); err == nil {
			mod = unq
		}
		return mod, true
	}
	return "", false
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}
