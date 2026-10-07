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

// namedArgumentPrograms pin labelled calls at the edges of evaluation and
// lowering: an argument that fails after an earlier one ran, a reordered
// row-polymorphic callback, and reordered bundled generic calls whose Go
// layouts are inferred from arguments in parameter order.
var namedArgumentPrograms = []struct{ name, source, output string }{
	{"failure.ef", `error Boom { at: string }

fn pair(first: string, second: string) -> string {
    first + "|" + second
}

effect fn mark(label: string) -> string uses { Console } {
    run Console.log("eval " + label)
    label
}

effect fn boom(label: string) -> string raises { Boom } uses { Console } {
    run Console.log("boom " + label)
    fail Boom { at: label }
}

effect fn body() -> string raises { Boom } uses { Console } {
    let a = pair(second: run mark("2"), first: run boom("1"))
    run Console.log("unreachable " + a)
    a
}

effect fn main() -> void {
    let r = run body().catch<Boom>("caught").provide<Console>(Stdout)
    run Console.log(r).provide<Console>(Stdout)
}
`, "eval 2\nboom 1\ncaught\n"},
	{"callback.ef", `effect fn mark(label: string) -> string uses { Console } {
    run Console.log("eval " + label)
    label
}

effect fn call<E: raises, R: uses>(callback: effect fn(string) -> string raises {E} uses {R}, input: string) -> string raises {E} uses {R} {
    run callback(input)
}

effect fn shout(text: string) -> string uses { Console } {
    run Console.log("shout " + text)
    text + "!"
}

effect fn body() -> void uses { Console } {
    let s = run call(input: run mark("in"), callback: shout)
    run Console.log(s)
}

effect fn main() -> void {
    run body().provide<Console>(Stdout)
}
`, "eval in\nshout in\nin!\n"},
	{"bundled.ef", `import Convert "effra/conversions"
import Fns "effra/functions"
record User { name: string }

effect fn decode(input: string) -> User uses { Console } {
    run Console.log("decode " + input)
    User { name: input }
}

effect fn encode(user: User) -> string uses { Console } {
    run Console.log("encode " + user.name)
    "<" + user.name + ">"
}

effect fn shout(text: string) -> string uses { Console } {
    run Console.log("shout " + text)
    text + "!"
}

fn pair(first: string, second: string) -> string {
    first + "|" + second
}

fn nested() -> string {
    pair(second: pair(second: "d", first: "c"), first: pair(second: "b", first: "a"))
}

effect fn body() -> void uses { Console } {
    let converter = Convert.witness(encode: encode, decode: decode)
    let user = run converter.decode("Ada")
    run Console.log(Fns.identity(input: run converter.encode(user)))
    run Console.log(nested())
    let r = run Fns.call(input: "z", callback: shout)
    run Console.log(r)
}

effect fn main() -> void {
    run body().provide<Console>(Stdout)
}
`, "decode Ada\nencode Ada\n<Ada>\na|b|c|d\nshout z\nz!\n"},
}

func TestNamedArgumentsUnderFailureCallbacksAndBundledGenerics(t *testing.T) {
	binary := buildTestCLI(t)
	root := t.TempDir()
	modules, err := filepath.Abs("../../node_modules")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(modules, filepath.Join(root, "node_modules")); err != nil {
		t.Fatal(err)
	}
	for _, program := range namedArgumentPrograms {
		file := filepath.Join(root, program.name)
		if err := os.WriteFile(file, []byte(program.source), 0600); err != nil {
			t.Fatal(err)
		}
		for _, target := range []string{"go", "js"} {
			stdout, stderr, code := runTestCLIDir(t, binary, root, "", "run", file, "--target", target)
			if code != 0 || len(stderr) != 0 || string(stdout) != program.output {
				t.Fatalf("%s on %s: code=%d stderr=%q\n%s\nwant\n%s", program.name, target, code, stderr, stdout, program.output)
			}
		}
	}
}
