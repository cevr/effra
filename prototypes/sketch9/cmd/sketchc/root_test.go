package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"effra.local/sketch9/sketch"
)

// writeTree writes files (path to content) under root.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// checkout writes a good checkout under a new temporary directory and
// returns its physical root.
func checkout(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	writeTree(t, root, map[string]string{
		"go.mod":                    "module " + sketch.KernelModule + "\n\ngo 1.27\n",
		"runtime/rt/rt.go":          "package rt\n",
		"prototypes/sketch9/go.mod": "module " + sketch.SketchModule + "\n\ngo 1.27\n",
	})
	return root
}

// The CLI's root: -rtroot when given, else the module that contains the
// source's physical file, whose problems are returned at once, else the
// working directory's module only when no module contains the source.
func TestFindKernelRoot(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no go tool")
	}
	noModule := func(t *testing.T) string {
		dir := t.TempDir()
		if gomod, found, _ := sketch.EnclosingModule(dir); found {
			t.Skipf("the temporary directory sits in the module %s", gomod)
		}
		return dir
	}
	for _, tc := range []struct {
		name string
		// setup returns the source file, the working directory, -rtroot and
		// the root the build must link ("" when it must fail).
		setup func(t *testing.T) (file, wd, rtroot, want string)
		fail  string // the substrings of the failure, joined by "|"
	}{
		{"source in the checkout", func(t *testing.T) (string, string, string, string) {
			root := checkout(t)
			writeTree(t, root, map[string]string{"apps/app.sketch": ""})
			return filepath.Join(root, "apps", "app.sketch"), noModule(t), "", root
		}, ""},
		{"source in sketch9's own module", func(t *testing.T) (string, string, string, string) {
			root := checkout(t)
			writeTree(t, root, map[string]string{"prototypes/sketch9/testdata/app.sketch": ""})
			return filepath.Join(root, "prototypes", "sketch9", "testdata", "app.sketch"), noModule(t), "", root
		}, ""},
		{"symlinked source", func(t *testing.T) (string, string, string, string) {
			root := checkout(t)
			writeTree(t, root, map[string]string{"apps/app.sketch": ""})
			link := filepath.Join(noModule(t), "apps-link")
			if err := os.Symlink(filepath.Join(root, "apps"), link); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(link, "app.sketch"), noModule(t), "", root
		}, ""},
		{"inner checkout declares no module", func(t *testing.T) (string, string, string, string) {
			outer := checkout(t)
			writeTree(t, outer, map[string]string{"inner/go.mod": "go 1.27\n", "inner/runtime/rt/rt.go": "package rt\n"})
			return filepath.Join(outer, "inner", "app.sketch"), outer, "", ""
		}, "declares no module|inner/go.mod|pass -rtroot"},
		{"inner checkout declares another module", func(t *testing.T) (string, string, string, string) {
			outer := checkout(t)
			writeTree(t, outer, map[string]string{"inner/go.mod": "module other.local/prototype\n", "inner/runtime/rt/rt.go": "package rt\n"})
			return filepath.Join(outer, "inner", "app.sketch"), outer, "", ""
		}, "declares module other.local/prototype|" + sketch.KernelModule},
		{"inner go.mod Go refuses", func(t *testing.T) (string, string, string, string) {
			outer := checkout(t)
			writeTree(t, outer, map[string]string{"inner/go.mod": "/*\nmodule " + sketch.KernelModule + "\n*/\n"})
			return filepath.Join(outer, "inner", "app.sketch"), outer, "", ""
		}, "mod files must use // comments"},
		{"no module contains the source", func(t *testing.T) (string, string, string, string) {
			root := checkout(t)
			return filepath.Join(noModule(t), "app.sketch"), filepath.Join(root, "runtime"), "", root
		}, ""},
		{"no module anywhere", func(t *testing.T) (string, string, string, string) {
			return filepath.Join(noModule(t), "app.sketch"), noModule(t), "", ""
		}, "no module contains|pass -rtroot"},
		{"-rtroot", func(t *testing.T) (string, string, string, string) {
			root := checkout(t)
			return filepath.Join(noModule(t), "app.sketch"), noModule(t), root, root
		}, ""},
		{"-rtroot with another module", func(t *testing.T) (string, string, string, string) {
			root := checkout(t)
			writeTree(t, root, map[string]string{"go.mod": "module other.local/prototype\n"})
			return filepath.Join(checkout(t), "app.sketch"), noModule(t), root, ""
		}, "-rtroot|declares module other.local/prototype"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, wd, rtroot, want := tc.setup(t)
			t.Chdir(wd)
			got, err := findKernelRoot(file, rtroot)
			if tc.fail == "" {
				if err != nil || got != want {
					t.Fatalf("findKernelRoot = %q, %v; want %s", got, err, want)
				}
				return
			}
			if err == nil {
				t.Fatalf("findKernelRoot = %q; want a failure naming %s", got, tc.fail)
			}
			for _, s := range strings.Split(tc.fail, "|") {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("failure %q lacks %q", err, s)
				}
			}
		})
	}
}
