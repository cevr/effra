package compiler

import (
	"os"
	"path/filepath"
	"testing"
)

const lifecycleProbe = `error Bad
service Gate { effect fn step(name: string) -> void }
effect fn worker() -> void uses {Gate} {run Gate.step("child")}
effect fn owned() -> string uses {Gate} {
 scope {
  run Gate.step("parent")
  let child = fork worker()
  run Gate.step("ready")
  "ok"
 }
}
effect fn broken() -> void raises {Bad} uses {Gate} {run Gate.step("announce");fail Bad}
effect fn unobserved() -> void raises {Bad} uses {Gate} {
 let child = fork broken()
 run Gate.step("announced")
}
effect fn timed() -> void raises {Timeout} uses {Gate, Scheduler} {run worker().timeout(20)}
effect fn recovered() -> void uses {Gate} {run timed().provide<Scheduler>(LiveScheduler).catch<Timeout>(void)}
effect fn main() -> void {
 // The Go and Effect probes run these programs directly. unobserved may
 // leave its child unobserved, which no callable value can carry, so its
 // recipe roots it instead of its function value.
 let probeOwned = owned
 let probeUnobserved = unobserved()
 let probeRecovered = recovered
}
`

func TestLifecycleConformanceAcrossGoAndEffect(t *testing.T) {
	r := Compile(lifecycleProbe)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	code, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = application.WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	probe := `package main
import("testing";"context";"errors";"reflect"; er "ergo.generated/runtime")
func TestLifecycle(t *testing.T){
 started:=make(chan struct{});order:=[]string{}
 provider:=efService_Gate{m_step:func(name string)efEffect[struct{}]{return func(ctx efContext)efExit[struct{}]{
  if name=="ready"{<-started;return er.Succeed(struct{}{})}
  acquired:=er.Invoke(ctx.Runtime,er.AcquireRelease(name,func(context.Context)(struct{},error){return struct{}{},nil},func(struct{},context.Context)error{order=append(order,name);return nil}))
  if acquired.IsFailure(){return acquired}
  if name=="child"{close(started);<-ctx.Runtime.Context().Done();return er.Interrupt[struct{}](ctx.Runtime.Context().Err())}
  return er.Succeed(struct{}{})
 }} }
 out:=er.Run(func(fc *er.FiberContext)er.Exit[string]{return efFunction_owned()(efContext{Runtime:fc,s_Gate:&provider})})
 if out.IsFailure() || !reflect.DeepEqual(order,[]string{"child","parent"}){t.Fatalf("ownership: %+v %v",out,order)}
 announced:=make(chan struct{})
 gate:=efService_Gate{m_step:func(name string)efEffect[struct{}]{return func(ctx efContext)efExit[struct{}]{if name=="announce"{close(announced)}else{<-announced};return er.Succeed(struct{}{})}}}
 unobserved:=er.Run(func(fc *er.FiberContext)er.Exit[struct{}]{return efFunction_unobserved()(efContext{Runtime:fc,s_Gate:&gate})})
 if unobserved.Failure==nil || unobserved.Failure.Tag!="Bad"{t.Fatalf("unobserved failure: %+v",unobserved)}
 leaking:=efService_Gate{m_step:func(string)efEffect[struct{}]{return func(ctx efContext)efExit[struct{}]{
  acquired:=er.Invoke(ctx.Runtime,er.AcquireRelease("resource",func(context.Context)(struct{},error){return struct{}{},nil},func(struct{},context.Context)error{return errors.New("cleanup defect")}))
  if acquired.IsFailure(){return acquired};<-ctx.Runtime.Context().Done();return er.Interrupt[struct{}](ctx.Runtime.Context().Err())
 }}}
 failed:=er.Run(func(fc *er.FiberContext)er.Exit[struct{}]{return efFunction_recovered()(efContext{Runtime:fc,s_Gate:&leaking})})
 if failed.Failure==nil || failed.Failure.Tag!="Timeout" || len(failed.Cause())!=2 || failed.Cause()[1].Kind!="defect"{t.Fatalf("timeout/catch erased cleanup: %+v",failed)}
}
`
	for name, data := range map[string][]byte{"go.mod": r.ModuleFile(), "main.go": []byte(code), "main_test.go": []byte(probe)} {
		if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	output, err := runGoCommand(dir, "test", "-race", ".")
	if err != nil {
		t.Fatalf("Go lifecycle: %v %s", err, output)
	}
}
