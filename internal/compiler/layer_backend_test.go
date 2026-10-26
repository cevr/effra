package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const layerApplicationSource = layerDiamondSource + `
effect fn names() -> string uses {Accounts,Invoice} {
 let account=run Accounts.name()
 let invoice=run Invoice.name()
 account+":"+invoice
}
layer Open { Accounts=AccountsLive }
effect fn borrowed() -> string uses {Database} { run Accounts.name().provide(Open) }
effect fn main() -> string {
 // The Go probe runs borrowed directly.
 let probeBorrowed = borrowed
 run names().provide(TestApp)
}
`

func TestLayerSourceProvisionExecutesSharedReplacementAndBorrowedInputAcrossTargets(t *testing.T) {
	r := CompileFor(layerApplicationSource, "go")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	source, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err = application.WriteRuntime(directory); err != nil {
		t.Fatal(err)
	}
	probe := `package main
import("testing";"context";"errors";"reflect"; er "effra.generated/runtime")
func TestLayerSource(t *testing.T){
 for range 2 {
  out:=er.Run(func(fc *er.FiberContext)er.Exit[string]{return efFunction_main()(efContext{Runtime:fc})})
  if out.IsFailure()||out.Value!="fixture:fixture"{t.Fatal(out)}
 }
 order:=[]string{}
 provider:=efService_Database{m_name:func()efEffect[string]{return func(ctx efContext)er.Exit[string]{
  acquired:=er.Invoke(ctx.Runtime,er.AcquireRelease("operation",func(context.Context)(struct{},error){return struct{}{},nil},func(struct{},context.Context)error{order=append(order,"program");return nil}))
  if acquired.IsFailure(){return er.Propagate[string](acquired)}
  return er.Succeed("borrowed")
 }}}
 out:=er.Run(func(fc *er.FiberContext)er.Exit[string]{
  root:=er.Invoke(fc,er.AcquireRelease("outer",func(context.Context)(struct{},error){return struct{}{},nil},func(struct{},context.Context)error{order=append(order,"outer");return nil}))
  if root.IsFailure(){return er.Propagate[string](root)}
  value:=efFunction_borrowed()(efContext{Runtime:fc,s_Database:&provider})
  if value.IsFailure()||value.Value!="borrowed"||!reflect.DeepEqual(order,[]string{"program"}){return er.Die[string](errors.New("program did not close before borrowed owner"))}
  return value
 })
 if out.IsFailure()||!reflect.DeepEqual(order,[]string{"program","outer"}){t.Fatal(out,order)}
}
`
	for name, data := range map[string][]byte{"go.mod": r.ModuleFile(), "main.go": []byte(source), "layer_test.go": []byte(probe)} {
		if err = os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := runGoCommand(directory, "test", "-race", "."); err != nil {
		t.Fatalf("native layer source: %v\n%s\n%s", err, output, source)
	}
	binary := filepath.Join(directory, "app")
	if output, err := runGoCommand(directory, "build", "-o", binary, "."); err != nil {
		t.Fatal(err, string(output))
	}
	if output, err := exec.Command(binary).CombinedOutput(); err != nil || string(output) != "fixture:fixture\n" {
		t.Fatal(err, string(output))
	}
}
