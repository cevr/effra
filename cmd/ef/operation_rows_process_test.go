package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The general callback form: a service operation with row parameters, an
// implementation renaming them, and a callback whose rows are instantiated at
// the call site. Both targets run the handler through the implementation.
const operationRowsSource = `record Req {
    path: string,
}
service Server {
    effect fn listen<E: raises, R: uses>(handler: effect fn(Req) -> string raises { E } uses { R }) -> void raises { E } uses { R }
}
impl Fake for Server {
    effect fn listen<A: raises, B: uses>(handler: effect fn(Req) -> string raises { A } uses { B }) -> void raises { A } uses { B } {
        let out = run handler(Req { path: "/x" })
        void
    }
}
effect fn route(r: Req) -> string uses { Console } {
    run Console.log(r.path)
    r.path
}
effect fn main() -> void {
    run Server.listen(route).provide<Server>(Fake).provide<Console>(Stdout)
}
`

func TestOperationRowParametersRunOnGoAndJS(t *testing.T) {
	runOperationRows(t, operationRowsSource, "/x\n")
}

// A concrete capture spelled like a method's row binder keeps both rows: the
// capture is provided at construction, the binder by the caller.
func TestOperationRowCaptureSharingABinderNameRuns(t *testing.T) {
	runOperationRows(t, `service C {
    effect fn ping() -> string
}
impl CP for C {
    effect fn ping() -> string {
        "pong"
    }
}
service S {
    effect fn go<R: uses>(h: effect fn() -> void uses { R }) -> void uses { R }
}
impl F for S uses { C } {
    effect fn go<C: uses>(h: effect fn() -> void uses { C }) -> void uses { C } {
        let p = run C.ping()
        run h()
    }
}
effect fn cb() -> void uses { Console } {
    run Console.log("x")
}
effect fn main() -> void {
    let f = run F().provide<C>(CP)
    run S.go(cb).provide<S>(f).provide<Console>(Stdout)
}
`, "x\n")
}

func TestProviderConfigurationCaptureWithBinderNameRuns(t *testing.T) {
	runOperationRows(t, `service S {
    effect fn go<R: uses>(h: effect fn() -> void uses { R }) -> void
}
impl F(config: effect fn() -> void uses { Console }) for S uses { Console } {
    effect fn go<Console: uses>(h: effect fn() -> void uses { Console }) -> void {
        run config()
    }
}
effect fn configured() -> void uses { Console } {
    run Console.log("x")
}
effect fn noop() -> void {
    void
}
effect fn main() -> void {
    let f = run F(configured).provide<Console>(Stdout)
    run S.go(noop).provide<S>(f)
}
`, "x\n")
}

func runOperationRows(t *testing.T, source, want string) {
	t.Helper()
	binary := buildTestCLI(t)
	root := t.TempDir()
	file := filepath.Join(root, "main.ef")
	if err := os.WriteFile(file, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	modules, err := filepath.Abs("../../node_modules")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(modules, filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"go", "js"} {
		stdout, stderr, code := runTestCLIDir(t, binary, root, "", "run", file, "--target", target)
		if code != 0 || string(stdout) != want || len(stderr) != 0 {
			t.Fatalf("%s: operation callback did not run: code=%d stdout=%q stderr=%q", target, code, stdout, stderr)
		}
	}
}
