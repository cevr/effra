package compiler

import (
	"os"
	"path/filepath"
	"testing"
)

// The emitted runtime package's own seam, since no language caller can cancel
// one waiter yet. Only the waiter's context is cancelled.
const layerWaiterSeamProbe = `package effra
import ("context";"testing";"time")
func TestLayerWaiterSeam(t *testing.T){
 for _,fails:=range []bool{false,true}{
  started,release:=make(chan context.Context,1),make(chan struct{})
  payload,constructions:=&struct{}{},0
  plan:=NewPlan("waiters",[]Node[int]{{Spec:NodeSpec{ID:"0/shared"},Construct:func(fc *FiberContext,s *int)Exit[Unit]{
   constructions++;started<-fc.Context();<-release
   if fails{return Fail[Unit]("ConfigError",payload)}
   *s=42;return Succeed(Unit{})
  }}},func(Unit)int{return 0},func(s *int)*int{return s})
  out:=Run(func(fc *FiberContext)Exit[Unit]{
   s:=0;build:=newLayerBuild(plan,&s,fc);build.start()
   evaluation:=<-started
   ctx,cancel:=context.WithCancel(context.Background())
   first,second:=make(chan Exit[Unit],1),make(chan Exit[Unit],1)
   go func(){first<-RunContext(ctx,func(w *FiberContext)Exit[Unit]{return build.awaitEntry(w,0)})}()
   go func(){second<-Run(func(w *FiberContext)Exit[Unit]{return build.awaitEntry(w,0)})}()
   for deadline:=time.Now().Add(3*time.Second);;time.Sleep(time.Millisecond){
    build.entries[0].outcome.mu.Lock();n:=len(build.entries[0].outcome.waiters);build.entries[0].outcome.mu.Unlock()
    if n==2{break}
    if time.Now().After(deadline){t.Fatal("waiters did not register")}
   }
   cancel()
   if out:=<-first;!out.Interrupted{t.Fatal("cancelled waiter",out)}
   if evaluation.Err()!=nil{t.Fatal("cancelling one waiter cancelled the build-owned producer")}
   close(release)
   observed,late:=<-second,build.awaitEntry(fc,0)
   build.await();cause:=build.constructionCause();build.close()
   if constructions!=1{t.Fatal("constructions",constructions)}
   if !fails{
    if observed.IsFailure()||late.IsFailure()||len(cause)!=0||s!=42{t.Fatal("shared success",observed,late,cause)}
   }else if observed.Failure==nil||late.Failure==nil||observed.Failure.Payload!=payload||late.Failure.Payload!=payload||len(cause)!=1||cause[0].Failure.Payload!=payload{
    t.Fatal("shared failure",observed,late,cause)
   }
   return Succeed(Unit{})
  })
  if out.IsFailure(){t.Fatal(out.Cause())}
 }
}
`

// Unlike Effect's MemoMap first-requester build, the producer belongs to the
// build: interrupting one waiter leaves the shared acquisition running.
func TestLayerEntryWaiterCancellationIsBuildOwnedAcrossTargets(t *testing.T) {
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
	for name, data := range map[string][]byte{"go.mod": r.ModuleFile(), "main.go": []byte(source), "runtime/layer_waiter_test.go": []byte(layerWaiterSeamProbe)} {
		if err = os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if output, err := runGoCommand(directory, "test", "-race", "-run", "^TestLayerWaiterSeam$", "./runtime"); err != nil {
		t.Fatalf("native waiter seam: %v\n%s", err, output)
	}
	output := runJS(t, layerApplicationSource, `
const deferred=()=>{let resolve;const promise=new Promise(done=>resolve=done);return {promise,resolve};};
for(const fails of [false,true]){
 const started=deferred(),release=deferred(),payload={_tag:'ConfigError'};
 let constructions=0,dependents=0,released=0,interrupted=false;
 const plan={id:'waiters',init:()=>({}),expose:state=>state,nodes:[
  {id:'0/shared',dependencies:[],construct:state=>Effect.gen(function*(){
   constructions++;
   yield* Effect.acquireRelease(Effect.void,()=>Effect.sync(()=>released++));
   started.resolve();
   yield* Effect.promise(()=>release.promise).pipe(Effect.onInterrupt(()=>Effect.sync(()=>interrupted=true)));
   if(fails)return yield* Effect.fail(payload);
   state.shared=42;
  })},
  {id:'1/dependent',dependencies:['0/shared'],construct:state=>Effect.sync(()=>{dependents++;state.dependent=state.shared;})},
 ]};
 const result=await Effect.runPromise(Effect.uninterruptibleMask(restore=>Effect.gen(function*(){
  const build=yield* __ef_layerBuild(plan);
  yield* build.start;
  yield* Effect.promise(()=>started.promise);
  const first=yield* Effect.forkDetach(build.await(0),{startImmediately:true}),second=yield* Effect.forkDetach(build.await(0),{startImmediately:true});
  yield* Effect.yieldNow;
  yield* Fiber.interrupt(first);
  const cancelled=yield* Fiber.await(first);
  if(!Exit.isFailure(cancelled)||!Cause.hasInterruptsOnly(cancelled.cause))throw new Error('cancelled waiter '+JSON.stringify(cancelled));
  release.resolve();
  const observed=yield* Fiber.await(second);
  // The second waiter's wake means the producer has exited.
  if(interrupted)throw new Error('cancelling one waiter interrupted the build-owned producer');
  const late=yield* Effect.exit(build.await(0));
  yield* build.settle(restore);
  const cause=build.cause(),cleanup=yield* build.close(Cause.empty);
  return {observed,late,cause,cleanup,state:build.state,dependentOwner:build.entries[1].owner};
 })));
 if(constructions!==1||released!==1||result.cleanup.reasons.length!==0)throw new Error('constructions '+constructions+' released '+released);
 if(!fails){
  if(!Exit.isSuccess(result.observed)||!Exit.isSuccess(result.late)||result.cause.reasons.length!==0||dependents!==1||result.state.dependent!==42)throw new Error('shared success');
  continue;
 }
 const error=exit=>Exit.isFailure(exit)&&exit.cause.reasons.length===1?exit.cause.reasons[0].error:undefined;
 if(error(result.observed)!==payload||error(result.late)!==payload||result.cause.reasons.length!==1||result.cause.reasons[0].error!==payload||dependents!==0||result.dependentOwner!==undefined)
  throw new Error('shared failure '+JSON.stringify({reasons:result.cause.reasons.length,dependents}));
}
console.log('entry waiters');
`)
	if output != "entry waiters\n" {
		t.Fatal(output)
	}
}

// Fiber.await registers through the producer Fiber's addObserver. Producers
// start from Effect's scheduler queue, after the caller's synchronous
// continuation, so counting begins before any waiter can register.
const jsLayerEntryWaiters = `
const deferred=()=>{let resolve;const promise=new Promise(done=>resolve=done);return {promise,resolve};};
const waiters=(fiber,count)=>new Promise((resolve,reject)=>{
 let registered=0;const add=fiber.addObserver.bind(fiber);
 fiber.addObserver=observer=>{if(++registered===count)resolve();return add(observer);};
 setTimeout(()=>reject(new Error('registered waiters '+registered+', want '+count)),3000).unref();
});
`

func TestLayerJSFailureWithConcurrentWaitersIsRetainedOnce(t *testing.T) {
	output := runJS(t, layerApplicationSource, jsLayerEntryWaiters+`
const release=deferred(),consumers=[],released=[];
const nodes=[{id:'0/database',dependencies:[],construct:()=>Effect.gen(function*(){
 yield* Effect.acquireRelease(Effect.void,()=>Effect.sync(()=>released.push('database')).pipe(Effect.flatMap(()=>Effect.die(new Error('database cleanup')))));
 yield* Effect.promise(()=>release.promise);
 return yield* Effect.fail({_tag:'DbError'});
})}];
for(const id of ['1/accounts','2/audit','3/billing'])nodes.push({id,dependencies:['0/database'],construct:()=>Effect.sync(()=>consumers.push(id))});
// The build seam __ef_provideLayer drives, so the test can hold the producer
// until every consumer waits on its pending entry.
const cause=await Effect.runPromise(Effect.uninterruptibleMask(restore=>Effect.gen(function*(){
 const build=yield* __ef_layerBuild({id:'shared-failure',init:()=>({}),nodes,expose:state=>state});
 yield* build.start;
 const registered=waiters(build.entries[0].fiber,3);
 yield* Effect.promise(()=>registered);
 release.resolve();
 yield* build.settle(restore);
 return yield* build.close(build.cause());
})));
const reasons=cause.reasons.map(reason=>reason._tag==='Fail'?reason.error._tag:reason._tag==='Die'?reason.defect.message:reason._tag);
if(JSON.stringify(reasons)!=='["DbError","database cleanup"]'||consumers.length||released.length!==1)throw new Error('shared failure '+JSON.stringify({reasons,consumers,released}));
console.log('shared failure');
`)
	if output != "shared failure\n" {
		t.Fatal(output)
	}
}

func TestLayerJSBuildCancellationReleasesPendingWaitersAndJoinsProducer(t *testing.T) {
	output := runJS(t, layerApplicationSource, jsLayerEntryWaiters+`
const ready=deferred(),canceled=deferred(),finish=deferred();
let settled=false,released=0,registered;const consumers=[];
const nodes=[{id:'0/database',dependencies:[],construct:()=>Effect.uninterruptibleMask(restore=>Effect.gen(function*(){
 yield* Effect.acquireRelease(Effect.void,()=>Effect.sync(()=>released++));
 yield* Effect.exit(restore(Effect.promise(signal=>{signal.addEventListener('abort',()=>canceled.resolve(),{once:true});ready.resolve();return new Promise(()=>{});})));
 yield* Effect.promise(()=>finish.promise);
 return yield* Effect.interrupt;
}))}];
for(const id of ['1/left','2/right'])nodes.push({id,dependencies:['0/database'],construct:()=>Effect.sync(()=>consumers.push(id))});
// The build seam __ef_provideLayer drives, so the test can interrupt only
// after both consumers wait on the pending entry.
const fiber=Effect.runFork(Effect.uninterruptibleMask(restore=>Effect.gen(function*(){
 const build=yield* __ef_layerBuild({id:'cancel',init:()=>({}),nodes,expose:state=>state});
 yield* build.start;
 registered=waiters(build.entries[0].fiber,2);
 yield* build.settle(restore);
 const cause=yield* build.close(build.cause());
 return yield* cause.reasons.length===0?Effect.void:Effect.failCause(cause);
})));
fiber.addObserver(()=>settled=true);
await ready.promise;await registered;fiber.interruptUnsafe();await canceled.promise;
await new Promise(resolve=>setTimeout(resolve,10));
if(settled||released)throw new Error('cancelled build returned before joining its producer');
finish.resolve();
const exit=await Effect.runPromise(Fiber.await(fiber));
// Abort-induced producer and waiter interruptions are control, not reasons.
if(!Exit.isFailure(exit)||exit.cause.reasons.length!==1||!Cause.hasInterruptsOnly(exit.cause)||consumers.length||released!==1)
 throw new Error('cancelled pending waiters '+JSON.stringify({reasons:Exit.isFailure(exit)?exit.cause.reasons.length:0,consumers,released}));
console.log('cancelled waiters');
`)
	if output != "cancelled waiters\n" {
		t.Fatal(output)
	}
}

func TestLayerJSShutdownClosesDependentsFirstAndAttemptsEveryFinalizer(t *testing.T) {
	output := runJS(t, layerApplicationSource, `
const closed=[];
const resource=(name,release)=>Effect.acquireRelease(Effect.void,()=>Effect.sync(()=>closed.push(name)).pipe(Effect.flatMap(()=>release)));
const node=(id,dependencies,first,second)=>({id,dependencies,construct:()=>Effect.gen(function*(){yield* resource(id+'.1',first);yield* resource(id+'.2',second);})});
const ok=Effect.void;
const nodes=[
 node('a/base',[],ok,ok),
 node('b/mid',['a/base'],Effect.sync(()=>{throw new Error('mid release panic');}),ok),
 node('c/top',['b/mid'],ok,Effect.die(new Error('top release failed'))),
 node('d/solo',[],ok,ok),
];
const exit=await Effect.runPromiseExit(__ef_provideLayer({id:'shutdown',init:()=>({}),nodes,expose:state=>state},()=>__ef_scoped(resource('program',ok))));
const reasons=Exit.isFailure(exit)?exit.cause.reasons.map(reason=>reason._tag==='Die'?reason.defect.message:reason._tag):[];
const want=['program','d/solo.2','d/solo.1','c/top.2','c/top.1','b/mid.2','b/mid.1','a/base.2','a/base.1'];
if(JSON.stringify(closed)!==JSON.stringify(want)||JSON.stringify(reasons)!=='["top release failed","mid release panic"]')
 throw new Error('shutdown '+JSON.stringify({closed,reasons}));
console.log('ordered shutdown');
`)
	if output != "ordered shutdown\n" {
		t.Fatal(output)
	}
}
