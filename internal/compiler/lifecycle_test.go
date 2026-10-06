package compiler

import (
	"os"
	"path/filepath"
	"testing"
)

const lifecycleProbe = `error Bad
service Gate { effect fn step(name: string) -> () }
effect fn worker() -> () uses {Gate} {run Gate.step("child")}
effect fn owned() -> string uses {Gate} {
 scope {
  run Gate.step("parent")
  let child = fork worker()
  run Gate.step("ready")
  "ok"
 }
}
effect fn broken() -> () raises {Bad} uses {Gate} {run Gate.step("announce");fail Bad}
effect fn unobserved() -> () raises {Bad} uses {Gate} {
 let child = fork broken()
 run Gate.step("announced")
}
effect fn timed() -> () raises {Timeout} uses {Gate} {run worker().timeout(20)}
effect fn recovered() -> () uses {Gate} {run timed().catch<Timeout>(())}
effect fn main() -> () {()}
`

func TestLifecycleConformanceAcrossGoAndEffect(t *testing.T) {
	r := Compile(lifecycleProbe)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	code, err := r.EmitGo()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = WriteRuntime(dir); err != nil {
		t.Fatal(err)
	}
	probe := `package main
import("testing";"context";"errors";"reflect"; er "effra.generated/runtime")
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
	js := runJS(t, lifecycleProbe, `
const Gate=__ef_service_Gate;
const order=[];let announce;const ready=new Promise(resolve=>announce=resolve);
const provider={step:name=>name==='ready' ? Effect.promise(()=>ready) : Effect.gen(function*(){
 yield* Effect.acquireRelease(Effect.void,()=>Effect.sync(()=>order.push(name)));
 if(name==='child'){announce();yield* Effect.never;}
})};
const owned=await Effect.runPromise(Effect.provideService(__ef_function_owned(),Gate,provider));
if(owned!=='ok'||JSON.stringify(order)!=='["child","parent"]')throw new Error('cleanup order '+order);
let announced;const signal=new Promise(resolve=>announced=resolve);
const failed=await Effect.runPromise(Effect.exit(Effect.provideService(__ef_function_unobserved(),Gate,{step:name=>name==='announce'?Effect.sync(()=>announced()):Effect.promise(()=>signal)})));
if(failed._tag!=='Failure'||failed.cause.reasons[0].error._tag!=='Bad')throw new Error('unobserved failure disappeared');
const leaking={step:()=>Effect.gen(function*(){yield* Effect.acquireRelease(Effect.void,()=>Effect.die(new Error('cleanup defect')));yield* Effect.never;})};
const timeout=await Effect.runPromise(Effect.exit(Effect.provideService(__ef_function_recovered(),Gate,leaking)));
if(timeout._tag!=='Failure'||timeout.cause.reasons.length!==2||timeout.cause.reasons[0].error._tag!=='Timeout'||timeout.cause.reasons[1]._tag!=='Die')throw new Error('timeout/catch erased cleanup '+JSON.stringify(timeout));
console.log('lifecycle conformance');
`)
	if js != "lifecycle conformance\n" {
		t.Fatalf("JS lifecycle: %s", js)
	}
}
