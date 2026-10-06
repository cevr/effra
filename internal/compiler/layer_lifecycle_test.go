package compiler

import "testing"

func TestLayerNodeOwnerRetainsScopeSignalAfterProducerAndThroughDependentClose(t *testing.T) {
	output := runJS(t, `effect fn main() -> () { () }`, `
const order=[];
let scopeSignal, operationSignal, ownReleaseSawScopeAlive=false;
const exit=await Effect.runPromise(Effect.exit(__ef_scoped(Effect.gen(function*(){
 const dependency=yield* __ef_openOwner;
 const producer=yield* Effect.forkDetach(__ef_useOwner(dependency,Effect.gen(function*(){
  scopeSignal=yield* Effect.abortSignal;
  yield* Effect.tryPromise(signal=>{operationSignal=signal;return Promise.resolve('acquired');});
  yield* Effect.acquireRelease(Effect.void,()=>Effect.sync(()=>{
   order.push('dependency');
   // Effect's earlier abortSignal finalizer follows later LIFO releases.
   ownReleaseSawScopeAlive=!scopeSignal.aborted;
  }));
  return 42;
 })),{uninterruptible:false});
 const constructed=yield* Fiber.await(producer);
 if(Exit.isFailure(constructed)||constructed.value!==42||scopeSignal.aborted||operationSignal.aborted)
  throw new Error('successful constructor aborted its captured signal');
 const dependent=yield* __ef_openOwner;
 yield* __ef_useOwner(dependent,Effect.acquireRelease(Effect.void,()=>Effect.sync(()=>{
  order.push('dependent');
  if(scopeSignal.aborted)throw new Error('dependency signal ended before dependent close');
 })));
 yield* (yield* __ef_closeOwner(dependent,Exit.succeed(undefined)));
 if(scopeSignal.aborted)throw new Error('dependent close cancelled dependency');
 yield* (yield* __ef_closeOwner(dependency,Exit.succeed(undefined)));
}))));
if(Exit.isFailure(exit)||!scopeSignal.aborted||!ownReleaseSawScopeAlive||operationSignal.aborted||JSON.stringify(order)!=='["dependent","dependency"]')
 throw new Error('ordered node owner signal lifecycle '+JSON.stringify(exit)+' '+order);
console.log('node owner signals');
`)
	if output != "node owner signals\n" {
		t.Fatal(output)
	}
}

func TestLayerPendingPromiseSignalAbortsBeforeNodeOwnerClose(t *testing.T) {
	output := runJS(t, `effect fn main() -> () { () }`, `
let scopeSignal, operationSignal, entered;
const ready=new Promise(resolve=>entered=resolve);
const exit=await Effect.runPromise(Effect.exit(__ef_scoped(Effect.gen(function*(){
 const node=yield* __ef_openOwner;
 const producer=yield* Effect.forkDetach(__ef_useOwner(node,Effect.gen(function*(){
  scopeSignal=yield* Effect.abortSignal;
  yield* Effect.tryPromise(signal=>{operationSignal=signal;entered();return new Promise(()=>{});});
 })),{uninterruptible:false});
 yield* Effect.promise(()=>ready);
 yield* Effect.sync(()=>producer.interruptUnsafe());
 const stopped=yield* Fiber.await(producer);
 if(!Exit.isFailure(stopped)||!Cause.hasInterruptsOnly(stopped.cause)||!operationSignal.aborted||scopeSignal.aborted)
  throw new Error('evaluation cancellation collapsed node ownership');
 yield* (yield* __ef_closeOwner(node,Exit.succeed(undefined)));
}))));
if(Exit.isFailure(exit)||!scopeSignal.aborted||!operationSignal.aborted)
 throw new Error('pending operation signal lifecycle '+JSON.stringify(exit));
console.log('pending promise signals');
`)
	if output != "pending promise signals\n" {
		t.Fatal(output)
	}
}
