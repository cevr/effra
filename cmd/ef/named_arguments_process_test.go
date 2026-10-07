package main

import (
	"os"
	"path/filepath"
	"testing"
)

// namedArgumentsSource binds labelled arguments to parameters by name across
// every call form that declares parameter names, while each `mark` trace shows
// that arguments still evaluate in source order.
const namedArgumentsSource = `record Card {
    title: string,
    body: string
}

service Pairs {
    effect fn join(left: string, right: string) -> string
}

impl Dash for Pairs {
    effect fn join(left: string, right: string) -> string {
        left + "-" + right
    }
}

impl Prefixed(prefix: string, suffix: string) for Pairs {
    effect fn join(left: string, right: string) -> string {
        prefix + left + right + suffix
    }
}

fn card(title: string, body: string) -> Card {
    Card {
        title,
        body
    }
}

fn pair(first: string, second: string) -> string {
    first + "|" + second
}

effect fn joined(first: string, second: string) -> string {
    first + "+" + second
}

effect fn mark(label: string) -> string uses { Console } {
    run Console.log("eval " + label)
    label
}

effect fn body() -> void uses { Console } {
    let c = card(body: "b", title: "t")
    run Console.log(c.title + c.body)
    let constructed = Card(body: "B", title: "T")
    run Console.log(constructed.title + constructed.body)
    let mixed = pair("m", second: "n")
    run Console.log(mixed)
    let a = pair(second: run mark("2"), first: run mark("1"))
    run Console.log(a)
    let b = run joined(second: run mark("y"), first: run mark("x"))
    run Console.log(b)
    let operation = run Pairs.join(right: run mark("r"), left: run mark("l")).provide<Pairs>(Dash)
    run Console.log(operation)
    let prefixed = run Prefixed(suffix: ">", prefix: "<")
    let configured = run Pairs.join("p", right: "q").provide<Pairs>(prefixed)
    run Console.log(configured)
}

effect fn main() -> void {
    run body().provide<Console>(Stdout)
}
`

const namedArgumentsOutput = `tb
TB
m|n
eval 2
eval 1
1|2
eval y
eval x
x+y
eval r
eval l
l-r
<pq>
`

func TestNamedArgumentsBindByNameAndEvaluateInSourceOrderOnBothTargets(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	file := filepath.Join(root, "named.ef")
	if err := os.WriteFile(file, []byte(namedArgumentsSource), 0600); err != nil {
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
		if code != 0 || len(stderr) != 0 || string(stdout) != namedArgumentsOutput {
			t.Fatalf("%s named arguments: code=%d stderr=%q\n%s\nwant\n%s", target, code, stderr, stdout, namedArgumentsOutput)
		}
	}
	// Formatting keeps each label with its value and the written order.
	formatted, stderr, code := runTestCLIInput(t, binary, namedArgumentsSource, "fmt", "--stdin")
	if code != 0 || len(stderr) != 0 || string(formatted) != namedArgumentsSource {
		t.Fatalf("formatter changed a labelled call: code=%d stderr=%q\n%s", code, stderr, formatted)
	}
}
