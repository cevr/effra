package compiler

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Each scenario drives the build seam that provision lowers to on both
// targets: the emitted Go runtime package and the JavaScript layer runtime.
// Both record one trace per scenario and compare it with the same expected
// string, so a target-specific ordering or cause difference fails here.
// Scenario keys name the behavior; traces use release/cancel markers and the
// complete Cause in order ("Fail:<tag>", "Die:<message>", "Interrupt").

const layerParityGoHelpers = `package effra
import ("context";"encoding/json";"errors";"fmt";"strings";"sync";"testing")
var _ = errors.New
var _ = fmt.Sprint
type parityTrace struct{mu sync.Mutex;events []string}
func (p *parityTrace) add(event string){p.mu.Lock();p.events=append(p.events,event);p.mu.Unlock()}
func (p *parityTrace) String() string{p.mu.Lock();defer p.mu.Unlock();return strings.Join(p.events,"|")}
func parityCause(cause Cause) string{
 parts:=[]string{}
 for _,reason:=range cause{
  switch reason.Kind{
  case "failure":parts=append(parts,"Fail:"+reason.Failure.Tag)
  case "defect":parts=append(parts,"Die:"+reason.Err.Error())
  default:parts=append(parts,"Interrupt")
  }
 }
 return strings.Join(parts,",")
}
func parityResource(fc *FiberContext,trace *parityTrace,name string,failure error)Exit[Unit]{
 return Invoke(fc,AcquireRelease(name,func(context.Context)(Unit,error){return Unit{},nil},func(Unit,context.Context)error{trace.add("release:"+name);return failure}))
}
func parityPlan[S any](id PlanID,nodes []Node[S],init func(Unit)S)Plan[Unit,S,*S]{
 return NewPlan(id,nodes,init,func(s *S)*S{return s})
}
func parityUnit(Unit)Unit{return Unit{}}
func checkParity(t *testing.T,wanted string,got map[string]string){
 want:=map[string]string{}
 if err:=json.Unmarshal([]byte(wanted),&want);err!=nil{t.Fatal(err)}
 for name,trace:=range want{
  if got[name]!=trace{t.Errorf("%s:\n got  %s\n want %s",name,got[name],trace)}
 }
 if len(got)!=len(want){t.Errorf("scenarios %v, want %v",got,want)}
}
`

const layerParityJSHelpers = `
const deferred=()=>{let resolve;const promise=new Promise(done=>resolve=done);return {promise,resolve};};
const parityCause=exit=>Exit.isFailure(exit)?exit.cause.reasons.map(reason=>reason._tag==='Fail'?'Fail:'+reason.error._tag:reason._tag==='Die'?'Die:'+reason.defect.message:'Interrupt').join(','):'';
const parityResource=(trace,name,failure)=>Effect.acquireRelease(Effect.void,()=>Effect.sync(()=>trace.push('release:'+name)).pipe(Effect.flatMap(()=>failure?Effect.die(new Error(failure)):Effect.void)));
const parityNode=(id,dependencies,construct)=>({id,dependencies,construct});
const parityPlan=(id,nodes)=>({id,init:()=>({}),nodes,expose:state=>state});
const checkParity=(want,got)=>{
 const failures=Object.keys(want).filter(name=>got[name]!==want[name]).map(name=>name+':\n got  '+got[name]+'\n want '+want[name]);
 if(failures.length||Object.keys(got).length!==Object.keys(want).length)throw new Error(failures.join('\n')+' scenarios '+JSON.stringify(got));
 console.log('parity');
};
`

// runLayerParity runs goProbe as TestLayerParityProbe inside the emitted Go
// runtime package with the race detector, and jsProbe against the emitted
// JavaScript module. Both receive want as WANT.
func runLayerParity(t *testing.T, want map[string]string, goProbe, jsProbe string) {
	t.Helper()
	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	r := CompileFor(layerApplicationSource, "go")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	_, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err = application.WriteRuntime(directory); err != nil {
		t.Fatal(err)
	}
	probe := layerParityGoHelpers + "const parityWant = " + "`" + string(encoded) + "`\n" + goProbe
	for name, data := range map[string][]byte{"go.mod": r.ModuleFile(), "runtime/layer_parity_test.go": []byte(probe)} {
		if err = os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := runGoCommand(directory, "test", "-race", "-count=1", "-run", "^TestLayerParityProbe$", "./runtime"); err != nil {
		t.Errorf("Go layer runtime: %v\n%s", err, output)
	}
	output := runJS(t, layerApplicationSource, layerParityJSHelpers+"const WANT="+string(encoded)+";\n"+jsProbe)
	if output != "parity\n" {
		t.Errorf("JavaScript layer runtime:\n%s", output)
	}
}

// Rollback, cancellation and cause composition. The canonical order case uses
// identities whose UTF-8 byte order (Go) and UTF-16 unit order differ.
func TestLayerRollbackCancellationAndCauseCompositionAcrossTargets(t *testing.T) {
	want := map[string]string{
		// A sibling fails while one dependent succeeded, one is still
		// constructing and a consumer waits on that pending entry: the pending
		// sibling observes cancellation, the waiter never constructs, rollback
		// releases dependents first, and the typed failure stays primary
		// beside every cleanup defect. Abort fabricates no failure.
		"partialFailureRollback": "cancelled:c|release:c|release:b|release:a|cause:Fail:ConfigError,Die:b cleanup,Die:a cleanup",
		// Caller cancellation while two producers run concurrently: a
		// cooperative producer stops, a masked acquisition completes late and
		// is released exactly once, nothing is released or returned before
		// both join, and the cause is the caller's interruption alone.
		"cancelDuringConcurrentAcquisition": "cancelled:b|finish|release:c|release:b|release:a|cause:Interrupt",
		// The provided program's failure and its own cleanup defect precede
		// node cleanup defects, which follow dependent-first close.
		"programFailureWithCleanupDefects": "release:program|release:b|release:a|cause:Fail:ProgramError,Die:program cleanup,Die:b cleanup,Die:a cleanup",
		// Canonical order is code point order: U+FFFF precedes U+10000.
		"canonicalCodePointOrder": "release:astral|release:bmp|cause:Fail:BMP,Fail:Astral",
	}
	runLayerParity(t, want, `
func TestLayerParityProbe(t *testing.T){
 got:=map[string]string{}
 {
  trace:=&parityTrace{};leftDone,rightReady:=make(chan struct{}),make(chan struct{})
  plan:=parityPlan("partial",[]Node[Unit]{
   {Spec:NodeSpec{ID:"a/base"},Construct:func(fc *FiberContext,_ *Unit)Exit[Unit]{return parityResource(fc,trace,"a",errors.New("a cleanup"))}},
   {Spec:NodeSpec{ID:"b/left",Dependencies:[]NodeID{"a/base"}},Construct:func(fc *FiberContext,_ *Unit)Exit[Unit]{
    out:=parityResource(fc,trace,"b",errors.New("b cleanup"));close(leftDone);return out}},
   {Spec:NodeSpec{ID:"c/right",Dependencies:[]NodeID{"a/base"}},Construct:func(fc *FiberContext,_ *Unit)Exit[Unit]{
    if out:=parityResource(fc,trace,"c",nil);out.IsFailure(){return out}
    close(rightReady);<-fc.Context().Done();trace.add("cancelled:c");return Interrupt[Unit](fc.Context().Err())}},
   {Spec:NodeSpec{ID:"d/fail",Dependencies:[]NodeID{"a/base"}},Construct:func(*FiberContext,*Unit)Exit[Unit]{
    <-leftDone;<-rightReady;return Fail[Unit]("ConfigError",Unit{})}},
   {Spec:NodeSpec{ID:"e/top",Dependencies:[]NodeID{"b/left","c/right"}},Construct:func(*FiberContext,*Unit)Exit[Unit]{trace.add("construct:e");return Succeed(Unit{})}},
  },parityUnit)
  out:=Run(Provide(plan,Unit{},func(*Unit)Effect[Unit]{trace.add("program");return func(*FiberContext)Exit[Unit]{return Succeed(Unit{})}}))
  trace.add("cause:"+parityCause(out.Cause()));got["partialFailureRollback"]=trace.String()
 }
 {
  trace:=&parityTrace{};ready,acquiring,cancelled,finish:=make(chan struct{}),make(chan struct{}),make(chan struct{}),make(chan struct{})
  plan:=parityPlan("cancel",[]Node[Unit]{
   {Spec:NodeSpec{ID:"a/base"},Construct:func(fc *FiberContext,_ *Unit)Exit[Unit]{return parityResource(fc,trace,"a",nil)}},
   {Spec:NodeSpec{ID:"b/never",Dependencies:[]NodeID{"a/base"}},Construct:func(fc *FiberContext,_ *Unit)Exit[Unit]{
    if out:=parityResource(fc,trace,"b",nil);out.IsFailure(){return out}
    close(ready);<-fc.Context().Done();trace.add("cancelled:b");close(cancelled);return Interrupt[Unit](fc.Context().Err())}},
   {Spec:NodeSpec{ID:"c/late",Dependencies:[]NodeID{"a/base"}},Construct:func(fc *FiberContext,_ *Unit)Exit[Unit]{
    return Invoke(fc,AcquireRelease("c",func(context.Context)(Unit,error){close(acquiring);<-finish;return Unit{},nil},func(Unit,context.Context)error{trace.add("release:c");return nil}))}},
   {Spec:NodeSpec{ID:"d/top",Dependencies:[]NodeID{"b/never","c/late"}},Construct:func(*FiberContext,*Unit)Exit[Unit]{trace.add("construct:d");return Succeed(Unit{})}},
  },parityUnit)
  ctx,cancel:=context.WithCancel(context.Background())
  done:=make(chan Exit[Unit],1)
  go func(){done<-RunContext(ctx,Provide(plan,Unit{},func(*Unit)Effect[Unit]{trace.add("program");return func(*FiberContext)Exit[Unit]{return Succeed(Unit{})}}))}()
  <-ready;<-acquiring;cancel();<-cancelled
  trace.add("finish");close(finish)
  out:=<-done
  trace.add("cause:"+parityCause(out.Cause()));got["cancelDuringConcurrentAcquisition"]=trace.String()
 }
 {
  trace:=&parityTrace{}
  plan:=parityPlan("program",[]Node[Unit]{
   {Spec:NodeSpec{ID:"a/base"},Construct:func(fc *FiberContext,_ *Unit)Exit[Unit]{return parityResource(fc,trace,"a",errors.New("a cleanup"))}},
   {Spec:NodeSpec{ID:"b/top",Dependencies:[]NodeID{"a/base"}},Construct:func(fc *FiberContext,_ *Unit)Exit[Unit]{return parityResource(fc,trace,"b",errors.New("b cleanup"))}},
  },parityUnit)
  out:=Run(Provide(plan,Unit{},func(*Unit)Effect[Unit]{return func(fc *FiberContext)Exit[Unit]{
   if out:=parityResource(fc,trace,"program",errors.New("program cleanup"));out.IsFailure(){return out}
   return Fail[Unit]("ProgramError",Unit{})}}))
  trace.add("cause:"+parityCause(out.Cause()));got["programFailureWithCleanupDefects"]=trace.String()
 }
 {
  trace:=&parityTrace{};readyBMP,readyAstral,barrier:=make(chan struct{}),make(chan struct{}),make(chan struct{})
  node:=func(id NodeID,name,tag string,ready chan struct{})Node[Unit]{return Node[Unit]{Spec:NodeSpec{ID:id},Construct:func(fc *FiberContext,_ *Unit)Exit[Unit]{
   if out:=parityResource(fc,trace,name,nil);out.IsFailure(){return out}
   close(ready);<-barrier;return Fail[Unit](tag,Unit{})}}}
  plan:=parityPlan("order",[]Node[Unit]{node("x\U00010000","astral","Astral",readyAstral),node("x\uFFFF","bmp","BMP",readyBMP)},parityUnit)
  done:=make(chan Exit[Unit],1)
  go func(){done<-Run(Provide(plan,Unit{},func(*Unit)Effect[Unit]{trace.add("program");return func(*FiberContext)Exit[Unit]{return Succeed(Unit{})}}))}()
  <-readyBMP;<-readyAstral;close(barrier)
  out:=<-done
  trace.add("cause:"+parityCause(out.Cause()));got["canonicalCodePointOrder"]=trace.String()
 }
 checkParity(t,parityWant,got)
}
`, `
const got={};
{
 const trace=[],leftDone=deferred(),rightReady=deferred();
 const plan=parityPlan('partial',[
  parityNode('a/base',[],()=>parityResource(trace,'a','a cleanup')),
  parityNode('b/left',['a/base'],()=>Effect.gen(function*(){yield* parityResource(trace,'b','b cleanup');leftDone.resolve();})),
  parityNode('c/right',['a/base'],()=>Effect.gen(function*(){yield* parityResource(trace,'c');rightReady.resolve();yield* Effect.never;}).pipe(Effect.onInterrupt(()=>Effect.sync(()=>trace.push('cancelled:c'))))),
  parityNode('d/fail',['a/base'],()=>Effect.gen(function*(){yield* Effect.promise(()=>Promise.all([leftDone.promise,rightReady.promise]));return yield* Effect.fail({_tag:'ConfigError'});})),
  parityNode('e/top',['b/left','c/right'],()=>Effect.sync(()=>trace.push('construct:e'))),
 ]);
 const exit=await Effect.runPromiseExit(__ef_provideLayer(plan,()=>Effect.sync(()=>trace.push('program'))));
 trace.push('cause:'+parityCause(exit));got.partialFailureRollback=trace.join('|');
}
{
 const trace=[],ready=deferred(),acquiring=deferred(),cancelled=deferred(),finish=deferred();
 const plan=parityPlan('cancel',[
  parityNode('a/base',[],()=>parityResource(trace,'a')),
  parityNode('b/never',['a/base'],()=>Effect.gen(function*(){yield* parityResource(trace,'b');ready.resolve();yield* Effect.never;}).pipe(Effect.onInterrupt(()=>Effect.sync(()=>{trace.push('cancelled:b');cancelled.resolve();})))),
  parityNode('c/late',['a/base'],()=>Effect.acquireRelease(Effect.promise(()=>{acquiring.resolve();return finish.promise;}),()=>Effect.sync(()=>trace.push('release:c')))),
  parityNode('d/top',['b/never','c/late'],()=>Effect.sync(()=>trace.push('construct:d'))),
 ]);
 const fiber=Effect.runFork(__ef_provideLayer(plan,()=>Effect.sync(()=>trace.push('program'))));
 await ready.promise;await acquiring.promise;fiber.interruptUnsafe();await cancelled.promise;
 trace.push('finish');finish.resolve();
 const exit=await Effect.runPromise(Fiber.await(fiber));
 trace.push('cause:'+parityCause(exit));got.cancelDuringConcurrentAcquisition=trace.join('|');
}
{
 const trace=[];
 const plan=parityPlan('program',[
  parityNode('a/base',[],()=>parityResource(trace,'a','a cleanup')),
  parityNode('b/top',['a/base'],()=>parityResource(trace,'b','b cleanup')),
 ]);
 const exit=await Effect.runPromiseExit(__ef_provideLayer(plan,()=>Effect.gen(function*(){yield* parityResource(trace,'program','program cleanup');return yield* Effect.fail({_tag:'ProgramError'});})));
 trace.push('cause:'+parityCause(exit));got.programFailureWithCleanupDefects=trace.join('|');
}
{
 const trace=[],readyBMP=deferred(),readyAstral=deferred(),barrier=deferred();
 const node=(id,name,tag,ready)=>parityNode(id,[],()=>Effect.uninterruptible(Effect.gen(function*(){
  yield* parityResource(trace,name);ready.resolve();yield* Effect.promise(()=>barrier.promise);return yield* Effect.fail({_tag:tag});
 })));
 const result=Effect.runPromiseExit(__ef_provideLayer(parityPlan('order',[node('x\u{10000}','astral','Astral',readyAstral),node('x\uFFFF','bmp','BMP',readyBMP)]),()=>Effect.sync(()=>trace.push('program'))));
 await readyBMP.promise;await readyAstral.promise;barrier.resolve();
 const exit=await result;
 trace.push('cause:'+parityCause(exit));got.canonicalCodePointOrder=trace.join('|');
}
checkParity(WANT,got);
`)
}

// A failed build keeps its one outcome; only a new explicit build retries,
// and the borrowed caller-owned resource stays open across both builds until
// its own owner closes. Separate builds never share acquisition.
func TestLayerFailedBuildRetriesOnlyThroughANewBuildAcrossTargets(t *testing.T) {
	want := map[string]string{
		"retryThroughNewBuild": "construct:a#1|release:a#1|first:Fail:ConfigError|construct:a#2|construct:b|program:outer|release:a#2|second:ok|release:outer",
	}
	runLayerParity(t, want, `
func TestLayerParityProbe(t *testing.T){
 got:=map[string]string{}
 trace:=&parityTrace{};attempts:=0
 type state struct{borrowed string}
 plan:=NewPlan[string,state,*state]("retry",[]Node[state]{
  {Spec:NodeSpec{ID:"a/flaky"},Construct:func(fc *FiberContext,_ *state)Exit[Unit]{
   attempts++;name:=fmt.Sprintf("a#%d",attempts);trace.add("construct:"+name)
   if out:=parityResource(fc,trace,name,nil);out.IsFailure(){return out}
   if attempts==1{return Fail[Unit]("ConfigError",Unit{})}
   return Succeed(Unit{})}},
  {Spec:NodeSpec{ID:"b/consumer",Dependencies:[]NodeID{"a/flaky"}},Construct:func(*FiberContext,*state)Exit[Unit]{trace.add("construct:b");return Succeed(Unit{})}},
 },func(borrowed string)state{return state{borrowed:borrowed}},func(s *state)*state{return s})
 program:=func(s *state)Effect[Unit]{return func(*FiberContext)Exit[Unit]{trace.add("program:"+s.borrowed);return Succeed(Unit{})}}
 out:=Run(func(fc *FiberContext)Exit[Unit]{
  if out:=parityResource(fc,trace,"outer",nil);out.IsFailure(){return out}
  first:=Invoke(fc,Provide(plan,"outer",program));trace.add("first:"+parityCause(first.Cause()))
  second:=Invoke(fc,Provide(plan,"outer",program))
  if second.IsFailure(){trace.add("second:"+parityCause(second.Cause()))}else{trace.add("second:ok")}
  return Succeed(Unit{})
 })
 if out.IsFailure(){t.Fatal(out.Cause())}
 got["retryThroughNewBuild"]=trace.String()
 checkParity(t,parityWant,got)
}
`, `
const got={},trace=[];let attempts=0;
const plan=parityPlan('retry',[
 parityNode('a/flaky',[],()=>Effect.gen(function*(){
  const name='a#'+(++attempts);trace.push('construct:'+name);
  yield* parityResource(trace,name);
  if(attempts===1)return yield* Effect.fail({_tag:'ConfigError'});
 })),
 parityNode('b/consumer',['a/flaky'],()=>Effect.sync(()=>trace.push('construct:b'))),
]);
const borrowed='outer';
const program=()=>Effect.sync(()=>trace.push('program:'+borrowed));
await Effect.runPromise(__ef_scoped(Effect.gen(function*(){
 yield* parityResource(trace,'outer');
 const first=yield* Effect.exit(__ef_provideLayer(plan,program));trace.push('first:'+parityCause(first));
 const second=yield* Effect.exit(__ef_provideLayer(plan,program));trace.push(Exit.isFailure(second)?'second:'+parityCause(second):'second:ok');
})));
got.retryThroughNewBuild=trace.join('|');
checkParity(WANT,got);
`)
}

// The runtime seam validates node and dependency identities before init or
// any constructor, with the same refusal on both targets. A cycle is a defect,
// never a build that waits forever.
func TestLayerMalformedPlansDefectBeforeCallbacksAcrossTargets(t *testing.T) {
	cases := map[string]string{
		"missingPlanIdentity":  "missing identity, initializer or output adapter",
		"tooManyNodes":         "more than 1000 nodes",
		"oversizedIdentity":    "metadata bound exceeded",
		"oversizedUTF8":        "metadata bound exceeded",
		"tooManyEdges":         "metadata bound exceeded",
		"emptyNodeIdentity":    "missing node identity/constructor or unsupported kind",
		"missingConstructor":   "missing node identity/constructor or unsupported kind",
		"unsupportedKind":      "missing node identity/constructor or unsupported kind",
		"duplicateIdentity":    "duplicate node identity",
		"unknownDependency":    "unknown or duplicate dependency",
		"duplicateDependency":  "unknown or duplicate dependency",
		"dependencyCycle":      "dependency cycle",
		"startupKindAccepted":  "ok",
		"utf8WithinMetadataOK": "ok",
		// Lone surrogates have no UTF-8 encoding; Go spells the same code
		// point's would-be bytes, which utf8.ValidString also refuses.
		"malformedPlanIdentity":       "identity is not well-formed Unicode",
		"loneLowSurrogateIdentity":    "identity is not well-formed Unicode",
		"loneHighSurrogateDependency": "identity is not well-formed Unicode",
		"astralIdentitiesAccepted":    "ok",
	}
	want := map[string]string{}
	for name, message := range cases {
		if message != "ok" {
			message = "Die:invalid layer plan: " + message
		}
		want[name] = message + "|callbacks:0"
	}
	// "é" is two UTF-8 bytes but one UTF-16 unit: 50001 of them exceed the
	// byte bound only when metadata is measured as Go measures it. 49990
	// stay within it once the plan and node framing are added.
	runLayerParity(t, want, `
func TestLayerParityProbe(t *testing.T){
 got:=map[string]string{}
 callbacks:=0
 construct:=func(*FiberContext,*Unit)Exit[Unit]{callbacks++;return Succeed(Unit{})}
 node:=func(id string,dependencies ...string)Node[Unit]{
  ids:=[]NodeID{};for _,dependency:=range dependencies{ids=append(ids,NodeID(dependency))}
  return Node[Unit]{Spec:NodeSpec{ID:NodeID(id),Dependencies:ids},Construct:construct}
 }
 many:=[]Node[Unit]{};for i:=0;i<1001;i++{many=append(many,node(fmt.Sprintf("n%d",i)))}
 wide:=[]Node[Unit]{};for i:=0;i<101;i++{wide=append(wide,node(fmt.Sprintf("w%d",i)))}
 fan:=[]string{};for _,n:=range wide{fan=append(fan,string(n.Spec.ID))}
 for i:=0;i<100;i++{wide=append(wide,node(fmt.Sprintf("f%d",i),fan...))}
 missing:=node("a");missing.Construct=nil
 startup:=node("s");startup.Spec.Kind=NodeStartup
 unsupported:=node("k");unsupported.Spec.Kind=NodeKind(7)
 plans:=map[string]struct{id PlanID;nodes []Node[Unit]}{
  "missingPlanIdentity":{"",[]Node[Unit]{node("a")}},
  "tooManyNodes":{"p",many},
  "oversizedIdentity":{"p",[]Node[Unit]{node(strings.Repeat("x",100001))}},
  "oversizedUTF8":{"p",[]Node[Unit]{node(strings.Repeat("é",50001))}},
  "tooManyEdges":{"p",wide},
  "emptyNodeIdentity":{"p",[]Node[Unit]{node("")}},
  "missingConstructor":{"p",[]Node[Unit]{missing}},
  "unsupportedKind":{"p",[]Node[Unit]{unsupported}},
  "duplicateIdentity":{"p",[]Node[Unit]{node("a"),node("a")}},
  "unknownDependency":{"p",[]Node[Unit]{node("a","missing")}},
  "duplicateDependency":{"p",[]Node[Unit]{node("a"),node("b","a","a")}},
  "dependencyCycle":{"p",[]Node[Unit]{node("a","b"),node("b","a"),node("c")}},
  "startupKindAccepted":{"p",[]Node[Unit]{startup}},
  "utf8WithinMetadataOK":{"p",[]Node[Unit]{node(strings.Repeat("é",49990))}},
  "malformedPlanIdentity":{"p\xed\xa0\x80",[]Node[Unit]{node("a")}},
  "loneLowSurrogateIdentity":{"p",[]Node[Unit]{node("x\xed\xb0\x80"),node("x\U00010000")}},
  "loneHighSurrogateDependency":{"p",[]Node[Unit]{node("a"),node("b","x\xed\xa0\x80")}},
  "astralIdentitiesAccepted":{"p",[]Node[Unit]{node("x\U00010000"),node("x\uFFFF","x\U00010000")}},
 }
 for name,spec:=range plans{
  callbacks=0
  plan:=NewPlan(spec.id,spec.nodes,func(Unit)Unit{callbacks++;return Unit{}},func(*Unit)Unit{return Unit{}})
  out:=Run(Provide(plan,Unit{},func(Unit)Effect[Unit]{return func(*FiberContext)Exit[Unit]{return Succeed(Unit{})}}))
  result:="ok"
  if out.IsFailure(){result=parityCause(out.Cause())}else{callbacks=0}
  got[name]=fmt.Sprintf("%s|callbacks:%d",result,callbacks)
 }
 checkParity(t,parityWant,got)
}
`, `
const got={};let callbacks=0;
const construct=()=>Effect.sync(()=>{callbacks++;});
const node=(id,...dependencies)=>({id,dependencies,construct});
const many=Array.from({length:1001},(_,i)=>node('n'+i));
const wide=Array.from({length:101},(_,i)=>node('w'+i));
const fan=wide.map(n=>n.id);
for(let i=0;i<100;i++)wide.push(node('f'+i,...fan));
const plans={
 missingPlanIdentity:['',[node('a')]],
 tooManyNodes:['p',many],
 oversizedIdentity:['p',[node('x'.repeat(100001))]],
 oversizedUTF8:['p',[node('é'.repeat(50001))]],
 tooManyEdges:['p',wide],
 emptyNodeIdentity:['p',[node('')]],
 missingConstructor:['p',[{id:'a',dependencies:[]}]],
 unsupportedKind:['p',[{...node('k'),kind:'worker'}]],
 duplicateIdentity:['p',[node('a'),node('a')]],
 unknownDependency:['p',[node('a','missing')]],
 duplicateDependency:['p',[node('a'),node('b','a','a')]],
 dependencyCycle:['p',[node('a','b'),node('b','a'),node('c')]],
 startupKindAccepted:['p',[{...node('s'),kind:'startup'}]],
 utf8WithinMetadataOK:['p',[node('é'.repeat(49990))]],
 malformedPlanIdentity:['p\uD800',[node('a')]],
 loneLowSurrogateIdentity:['p',[node('x\uDC00'),node('x\u{10000}')]],
 loneHighSurrogateDependency:['p',[node('a'),node('b','x\uD800')]],
 astralIdentitiesAccepted:['p',[node('x\u{10000}'),node('x\uFFFF','x\u{10000}')]],
};
for(const [name,[id,nodes]] of Object.entries(plans)){
 callbacks=0;
 const exit=await Effect.runPromiseExit(__ef_provideLayer({id,nodes,init:()=>{callbacks++;return {};},expose:state=>state},()=>Effect.void));
 if(Exit.isSuccess(exit))callbacks=0;
 got[name]=(Exit.isFailure(exit)?parityCause(exit):'ok')+'|callbacks:'+callbacks;
}
checkParity(WANT,got);
`)
}
