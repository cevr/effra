package compiler

import (
	"fmt"
	"strings"
	"testing"
)

// These controls distinguish callback-result evidence from borrowing the
// callable parameter itself. Both callbacks have assignable public contracts.
func TestCallbackResultOwnershipAcrossHelpersAndOrders(t *testing.T) {
	declarations := []string{
		`effect fn forward(cb: effect fn(File)->File raises {IoError} uses {Files}, file:File)->File raises {IoError} uses {Files}{run relay(cb,file)}`,
		`effect fn relay(cb: effect fn(File)->File raises {IoError} uses {Files}, file:File)->File raises {IoError} uses {Files}{run cb(file)}`,
		`effect fn keep(file:File)->File{file}`,
		`effect fn acquire(file:File)->File raises {IoError} uses {Files}{run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)}`,
	}
	for _, order := range [][]int{{0, 1, 2, 3}, {3, 2, 1, 0}} {
		prefix := ""
		for _, index := range order {
			prefix += declarations[index] + "\n"
		}
		for _, test := range []struct {
			name, callee string
			valid        bool
		}{
			{"borrowed", "keep", true},
			{"acquired", "acquire", false},
			{"conditional acquired", "if true {keep} else {acquire}", false},
		} {
			t.Run(test.name+"/"+declarations[order[0]][10:17], func(t *testing.T) {
				source := prefix + `effect fn outer(file:File)->File raises {IoError} uses {Files}{let chosen=` + test.callee + `;scope {run forward(chosen,file)}} effect fn main()->void{void}`
				r := Compile(source)
				if r.Checked != test.valid || (!test.valid && !hasCode(r, "EF123")) {
					t.Fatalf("callback result ownership: %+v", r.Diagnostics)
				}
				if test.valid {
					info, err := r.TypeAt(strings.Index(source, "run forward(chosen"))
					if err != nil {
						t.Fatal(err)
					}
					if len(info.Type.Ownership) != 1 || info.Type.Ownership[0].Status != "borrowed" {
						t.Fatalf("borrowed result proof lost: %+v", info.Type.Ownership)
					}
				}
			})
		}
	}
}

func TestUnresolvedCallbackResultsStayPotential(t *testing.T) {
	source := `record Operations { operation: effect fn(File)->File }
effect fn outer(operations:Operations,file:File)->File {scope {run operations.operation(file)}}
effect fn main()->void{void}`
	r := Compile(source)
	if r.Checked || !hasCode(r, "EF123") {
		t.Fatalf("unresolved record callback escaped: %+v", r.Diagnostics)
	}
}

func TestNamedCallbackAcquisitionDependencyUsesValueReferences(t *testing.T) {
	source := `effect fn outer(file:File)->File raises {IoError} {scope {run reopen(file)}}
effect fn reopen(file:File)->File raises {IoError} {let operation=acquire;run operation(file)}
effect fn acquire(file:File)->File raises {IoError} {run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)}
effect fn main()->void{void}`
	r := Compile(source)
	if r.Checked || !hasCode(r, "EF123") {
		t.Fatalf("forward value reference lost acquisition: %+v", r.Diagnostics)
	}
}

func TestCallbackResultOwnershipSurvivesFunctionReturningHelpers(t *testing.T) {
	for _, test := range []struct {
		name, body string
		valid      bool
	}{
		{"borrowed", `file`, true},
		{"acquired", `run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := `fn identity(cb:effect fn(File)->File raises {IoError})->(effect fn(File)->File raises {IoError}){cb}
effect fn outer(file:File)->File raises {IoError}{let operation=identity(chosen);scope {run operation(file)}}
effect fn chosen(file:File)->File raises {IoError}{` + test.body + `}
effect fn main()->void{void}`
			r := Compile(source)
			if r.Checked != test.valid || (!test.valid && !hasCode(r, "EF123")) {
				t.Fatalf("function-valued return lost ownership relation: %+v", r.Diagnostics)
			}
		})
	}
}

func TestGenericCallbackCannotPromiseSafeResultFromInnerScope(t *testing.T) {
	r := Compile(`effect fn scoped(cb:effect fn(File)->File,file:File)->File{scope {run cb(file)}} effect fn main()->void{void}`)
	if r.Checked || !hasCode(r, "EF123") {
		t.Fatalf("unresolved callback obligation discharged by closing scope: %+v", r.Diagnostics)
	}
}

func TestUnresolvedCallbackResultsRetainChildAndTimeoutOwners(t *testing.T) {
	for _, body := range []string{`run cb(file).timeout(1000)`, `let child=fork cb(file);run child.join()`} {
		r := Compile(`effect fn bounded(cb:effect fn(File)->File,file:File)->File raises {Timeout} uses {Scheduler}{` + body + `} effect fn main()->void{void}`)
		if r.Checked || !hasCode(r, "EF123") {
			t.Fatalf("managed callback result lost closing owner: %+v", r.Diagnostics)
		}
	}
}

func TestNamedCallbackEvidenceLimitFailsConservatively(t *testing.T) {
	for _, count := range []int{8, 9} {
		var source strings.Builder
		for i := 0; i < count; i++ {
			fmt.Fprintf(&source, "effect fn callback%d(file:File)->File{file}\n", i)
		}
		branch := fmt.Sprintf("callback%d", count-1)
		for i := count - 2; i >= 0; i-- {
			branch = fmt.Sprintf("if true {callback%d} else {%s}", i, branch)
		}
		fmt.Fprintf(&source, "effect fn outer(file:File)->File{let callback=%s;scope {run callback(file)}} effect fn main()->void{void}", branch)
		r := Compile(source.String())
		if count == 8 && !r.Checked {
			t.Fatalf("finite known borrowed set refused: %+v", r.Diagnostics)
		}
		if count == 9 && (r.Checked || !hasCode(r, "EF123")) {
			t.Fatalf("exhausted callback evidence became harmless unknown: %+v", r.Diagnostics)
		}
	}
}
