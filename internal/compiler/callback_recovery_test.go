package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestRecoveredCallableOwnershipJoinsEveryAlternative(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		for _, test := range []struct {
			name, success, fallback string
			helper, valid           bool
		}{
			{"borrowed", "keep", "otherKeep", false, true},
			{"acquiring fallback", "keep", "acquire", false, false},
			{"acquiring success", "acquire", "keep", false, false},
			{"parameter fallback", "keep", "acquire", true, false},
		} {
			t.Run(fmt.Sprintf("%s/reverse=%t", test.name, reverse), func(t *testing.T) {
				declarations := []string{
					`error Missing`,
					`effect fn keep(file:File)->File raises {IoError}{file}`,
					`effect fn otherKeep(file:File)->File raises {IoError}{file}`,
					`effect fn acquire(file:File)->File raises {IoError}{run Files.openRead("examples/fixture.txt").provide<Files>(LiveFiles)}`,
					`effect fn choose()->(effect fn(File)->File raises {IoError}) raises {Missing}{if true {fail Missing} else {` + test.success + `}}`,
				}
				chosen := `run choose().catch<Missing>(` + test.fallback + `)`
				if test.helper {
					declarations = append(declarations, `effect fn recover(fallback:effect fn(File)->File raises {IoError})->(effect fn(File)->File raises {IoError}){run choose().catch<Missing>(fallback)}`)
					chosen = `run recover(` + test.fallback + `)`
				}
				declarations = append(declarations, `effect fn outer(file:File)->File raises {IoError}{let operation=`+chosen+`;scope {run operation(file)}}`, `effect fn main()->(){()}`)
				if reverse {
					slices.Reverse(declarations)
				}
				r := Compile(strings.Join(declarations, "\n"))
				if r.Checked != test.valid || (!test.valid && !hasCode(r, "EF123")) {
					t.Fatalf("recovered callback evidence: %+v", r.Diagnostics)
				}
				if test.valid {
					symbol := r.Find("outer")
					if len(symbol.Actual.Ownership) != 1 || symbol.Actual.Ownership[0].Status != "borrowed" {
						t.Fatalf("borrowed recovery lost proof: %+v", symbol.Actual)
					}
				}
			})
		}
	}
}

func TestCallableFactoryPreservesValueAndEvaluationLevels(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "examples", "callables-factory.ef"))
	if err != nil {
		t.Fatal(err)
	}
	source := string(fixture)
	r := Compile(source)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	for _, test := range []struct {
		name               string
		failures, services []string
	}{
		{"pureFailure", []string{"Missing"}, nil}, {"pureService", nil, []string{"Logger"}}, {"pureEmpty", nil, nil}, {"effectEmpty", nil, nil}, {"factory", []string{"Missing"}, []string{"Logger"}}, {"nestedFactory", []string{"Missing"}, nil},
	} {
		symbol := r.Find(test.name)
		p := r.ProjectSymbol(symbol)
		if !p.Complete {
			t.Fatal(p.Error)
		}
		actual := symbol.Actual
		if !slices.Equal(actual.Evaluation.Failures, test.failures) || !slices.Equal(actual.Evaluation.Requirements, test.services) {
			t.Fatalf("%s evaluation: %+v", test.name, actual)
		}
		nodes := map[string]TypeNode{}
		for _, n := range p.Types {
			nodes[n.ID] = n
		}
		node := nodes[actual.Contract.ID]
		if len(test.failures)+len(test.services) > 0 {
			if node.Kind != "recipe" || !slices.Equal(actual.Errors, test.failures) || !slices.Equal(actual.Services, test.services) || actual.Callable != nil {
				t.Fatalf("%s outer invocation: %+v", test.name, actual)
			}
			node = nodes[node.Result]
		}
		if node.Kind != "callable" {
			t.Fatalf("%s lost returned callable: %+v", test.name, node)
		}
		if test.name == "factory" || test.name == "effectEmpty" {
			rows := map[string][]string{}
			for _, row := range p.Rows {
				rows[row.ID] = row.Labels
			}
			if node.Mode != "effect" || !slices.Equal(rows[node.FailureRow], []string{"Broken"}) || !slices.Equal(rows[node.ServiceRow], []string{"Directory"}) {
				t.Fatalf("inner callback rows erased: %+v", node)
			}
		} else if node.Mode != "pure" || node.FailureRow != "" || node.ServiceRow != "" {
			t.Fatalf("pure result was recontracted: %+v", node)
		}
		if test.name == "nestedFactory" && nodes[node.Result].Kind != "callable" {
			t.Fatal("nested callable result stripped")
		}
	}
	for _, test := range []struct{ name, old, new, code string }{
		{"outer failure", "raises { Missing } uses { Logger }", "uses { Logger }", "EF107"},
		{"outer service", "raises { Missing } uses { Logger }", "raises { Missing }", "EF108"},
		{"inner failure", `.catch<Broken>("broken")`, "", "EF107"},
		{"inner service", `.provide<Directory>(Names)`, "", "EF108"},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := Compile(strings.Replace(source, test.old, test.new, 1))
			if bad.Checked || !hasCode(bad, test.code) {
				t.Fatalf("missing contract admitted: %+v", bad.Diagnostics)
			}
		})
	}
	_, declaration, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	checkStrictTypeScript(t, declaration, `import {factory, pureFailure, pureService, pureEmpty, nestedFactory} from "./generated.mjs";
import type {DirectoryRequirement,LoggerRequirement} from "./generated.mjs";
import type {Effect} from "effect";
type Callback=(x:string)=>Effect.Effect<string,{readonly _tag:"Broken"},DirectoryRequirement>;
type Expected=Effect.Effect<Callback,{readonly _tag:"Missing"},LoggerRequirement>;
const actual=factory(); const exact:Expected=actual;declare const expected:Expected;const reciprocal:typeof actual=expected;
// @ts-expect-error Both semantic levels must be retained.
const erased:Effect.Effect<(x:string)=>Effect.Effect<string,never,never>,never,never>=actual;
const failure:Effect.Effect<(x:string)=>string,{readonly _tag:"Missing"},never>=pureFailure();
const service:Effect.Effect<(x:string)=>string,never,LoggerRequirement>=pureService();
const empty:Effect.Effect<(x:string)=>string,never,never>=pureEmpty();
const nested:Effect.Effect<(x:string)=>(y:string)=>string,{readonly _tag:"Missing"},never>=nestedFactory();
void exact;void reciprocal;void erased;void failure;void service;void empty;void nested;`)
	generated, err := r.EmitGo()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"go.mod": string(r.ModuleFile()), "main.go": generated} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := runGoCommand(dir, "run", "."); err != nil || string(output) != "name:42\n" {
		t.Fatalf("native returned callbacks: %v %s", err, output)
	}
	if output := runJS(t, source, `console.log(await Effect.runPromise(__ef_function_main()));`); output != "name:42\n" {
		t.Fatalf("JS returned callbacks: %s", output)
	}
}
