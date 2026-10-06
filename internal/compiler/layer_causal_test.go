package compiler

import "testing"

func TestLayerJSAdmissionSurvivesSchedulerYields(t *testing.T) {
	output := runJS(t, layerApplicationSource, `
for(const dependent of [false,true])for(const budget of [8,16,32,64]){
 let afterFailure=0,failed=false,programs=0,closed=0,ready;
 const entered=new Promise(resolve=>ready=resolve);
 const dependencies=dependent?['1']:[];
 const nodes=[{id:'0',dependencies:[],construct:()=>Effect.gen(function*(){
  yield* Effect.acquireRelease(Effect.void,()=>Effect.sync(()=>closed++));
  return yield* Effect.promise(signal=>{signal.addEventListener('abort',()=>failed=true,{once:true});ready();return new Promise(()=>{});});
 })},
 {id:'a',dependencies,construct:()=>Effect.gen(function*(){
  yield* Effect.acquireRelease(Effect.void,()=>Effect.sync(()=>closed++));
  yield* Effect.promise(()=>entered);return yield* Effect.fail({_tag:'ConfigError'});
 })}];
 if(dependent)nodes.push({id:'1',dependencies:[],construct:()=>Effect.void});
 for(let index=0;index<64;index++)nodes.push({id:'b'+String(index).padStart(2,'0'),dependencies,construct:()=>Effect.sync(()=>{if(failed)afterFailure++;})});
 const recipe=Effect.provideService(__ef_provideLayer({id:'admission',init:()=>({}),nodes,expose:state=>state},()=>Effect.sync(()=>programs++)),Scheduler.MaxOpsBeforeYield,budget);
 let timer;
 const deadline=new Promise((_,reject)=>{timer=setTimeout(()=>reject(new Error('admission failed to complete under scheduler budget '+budget)),5000);});
 let exit;
 try{exit=await Promise.race([Effect.runPromiseExit(recipe),deadline]);}finally{clearTimeout(timer);}
 if(!Exit.isFailure(exit)||exit.cause.reasons.length!==1||exit.cause.reasons[0]._tag!=='Fail'||exit.cause.reasons[0].error._tag!=='ConfigError'||programs!==0||afterFailure!==0||closed!==2)throw new Error('scheduler admission '+budget+' '+JSON.stringify(exit)+' later='+afterFailure+' closed='+closed);
}
console.log('scheduler admission');
`)
	if output != "scheduler admission\n" {
		t.Fatal(output)
	}
}

func TestLayerJSManagedCausalRollback(t *testing.T) {
	output := runJS(t, layerApplicationSource, `
const deferred=()=>{let resolve;const promise=new Promise(done=>resolve=done);return {promise,resolve};};
const node=(id,dependencies,construct)=>({id,dependencies,construct});
const plan=nodes=>({id:'control',init:()=>({}),nodes,expose:state=>state});
{
 const counts=[0,0,0,0];
 const recipe=__ef_provideLayer(plan([
  node('a',[],state=>Effect.gen(function*(){counts[0]++;state.shared={signal:yield* Effect.abortSignal};})),
  node('b',['a'],state=>Effect.gen(function*(){
   counts[1]++;state.left=state.shared;
   yield* Effect.acquireRelease(Effect.void,()=>Effect.sync(()=>{if(state.shared.signal.aborted)throw new Error('dependency signal ended before dependent cleanup');}));
  })),
  node('c',['a'],state=>Effect.sync(()=>{counts[2]++;state.right=state.shared;})),
  node('d',[],()=>Effect.sync(()=>counts[3]++)),
 ]),state=>Effect.sync(()=>{if(state.left!==state.right||state.shared.signal.aborted)throw new Error('diamond identity or node signal lifetime');return state.shared;}));
 if(counts.some(Boolean))throw new Error('layer recipe constructed eagerly');
 const first=await Effect.runPromise(recipe),second=await Effect.runPromise(recipe);
 if(first===second||!first.signal.aborted||!second.signal.aborted||counts.some(count=>count!==2))throw new Error('build freshness, signal close or selected disconnected node pruned');
}
{
 const ready=deferred(),canceled=deferred(),finish=deferred();let settled=false,closed=0;
 const recipe=__ef_provideLayer(plan([node('a',[],()=>Effect.uninterruptibleMask(restore=>Effect.gen(function*(){
  yield* Effect.acquireRelease(Effect.void,()=>Effect.sync(()=>closed++));
  yield* Effect.exit(restore(Effect.promise(signal=>{signal.addEventListener('abort',()=>canceled.resolve(),{once:true});ready.resolve();return new Promise(()=>{});})));
  yield* Effect.promise(()=>finish.promise);
  return yield* Effect.fail({_tag:'ActualFailure'});
 })))]),()=>Effect.void);
 const fiber=Effect.runFork(recipe);
 fiber.addObserver(()=>settled=true);
 await ready.promise;fiber.interruptUnsafe();await canceled.promise;
 if(settled||closed)throw new Error('caller interruption returned before joining producer');
 finish.resolve();
 const exit=await Effect.runPromise(Fiber.await(fiber));
 if(!Exit.isFailure(exit)||exit.cause.reasons[0]._tag!=='Interrupt'||!exit.cause.reasons.some(reason=>reason._tag==='Fail'&&reason.error._tag==='ActualFailure')||closed!==1)throw new Error('caller interruption lost actual cause or cleanup');
}
// A cooperative peer observes build cancellation before masked acquisition is released.
{
 const entered=deferred(), cooperative=deferred(), canceled=deferred(), release=deferred();
 const order=[];let settled=false, programs=0;
 const recipe=__ef_provideLayer(plan([
  node('a',[],()=>Effect.acquireRelease(Effect.void,()=>Effect.sync(()=>order.push('dependency')))),
  node('b',['a'],()=>Effect.acquireRelease(Effect.promise(()=>{entered.resolve();return release.promise;}),()=>Effect.sync(()=>order.push('late')))),
  node('c',[],()=>Effect.promise(signal=>{signal.addEventListener('abort',()=>canceled.resolve(),{once:true});cooperative.resolve();return new Promise(()=>{});})),
  node('d',[],()=>Effect.gen(function*(){yield* Effect.promise(()=>Promise.all([entered.promise,cooperative.promise]));return yield* Effect.fail({_tag:'ConfigError'});})),
 ]),()=>Effect.sync(()=>programs++));
 const result=Effect.runPromiseExit(recipe).then(exit=>{settled=true;return exit;});
 await canceled.promise;
 if(settled||order.length)throw new Error('late acquisition was not joined before rollback');
 release.resolve('resource');
 const exit=await result;
 if(!Exit.isFailure(exit)||exit.cause.reasons.length!==1||exit.cause.reasons[0].error._tag!=='ConfigError'||programs!==0||JSON.stringify(order)!=='["late","dependency"]')throw new Error('late rollback '+JSON.stringify(exit)+' '+order);
}
// Release either real failure first; peer interruption is observed before its protected failure.
for(const first of [0,1]){
 const ready=[deferred(),deferred()], evaluations=[deferred(),deferred()], failures=[deferred(),deferred()], canceled=[deferred(),deferred()];
 let consumer=0;
 const recipe=__ef_provideLayer(plan(['a','b'].map((id,index)=>node(id,[],()=>Effect.uninterruptibleMask(restore=>Effect.gen(function*(){
  yield* Effect.acquireRelease(Effect.void,()=>Effect.die(new Error(id+' cleanup')));
  yield* Effect.exit(restore(Effect.promise(signal=>{signal.addEventListener('abort',()=>canceled[index].resolve(),{once:true});ready[index].resolve();return evaluations[index].promise;})));
  yield* Effect.promise(()=>failures[index].promise);
  return yield* Effect.fail({_tag:id});
})))).concat([node('z',['a','b'],()=>Effect.sync(()=>consumer++))])),()=>Effect.void);
 const result=Effect.runPromiseExit(recipe);
 await Promise.all(ready.map(value=>value.promise));
 evaluations[first].resolve();failures[first].resolve();
 await canceled[1-first].promise;
 failures[1-first].resolve();
 const exit=await result;
 const reasons=exit.cause.reasons.map(reason=>reason._tag==='Fail'?reason.error._tag:reason.defect.message);
 if(!Exit.isFailure(exit)||consumer!==0||JSON.stringify(reasons)!=='["a","b","b cleanup","a cleanup"]')throw new Error('concurrent actual failures '+JSON.stringify(reasons));
}
{
 let closed=0,consumer=0;
 const exit=await Effect.runPromiseExit(__ef_provideLayer(plan([
  node('a',[],()=>Effect.gen(function*(){yield* Effect.acquireRelease(Effect.void,()=>Effect.sync(()=>closed++));throw new Error('constructor panic');})),
  node('b',['a'],()=>Effect.sync(()=>consumer++)),
 ]),()=>Effect.void));
 if(!Exit.isFailure(exit)||exit.cause.reasons[0]._tag!=='Die'||closed!==1||consumer!==0)throw new Error('constructor panic did not join and close');
}
console.log('causal rollback');
`)
	if output != "causal rollback\n" {
		t.Fatal(output)
	}
}
