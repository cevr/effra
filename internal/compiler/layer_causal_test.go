package compiler

import "testing"

func TestLayerJSRetainsEqualReasonsWithinOneProducer(t *testing.T) {
	output := runJS(t, layerApplicationSource, `
const error={_tag:'ConfigError'},original=Cause.fromReasons([Cause.fail(error).reasons[0],Cause.fail(error).reasons[0]]);
let programs=0,recovered=0;
const recipe=__ef_provideLayer({id:'composite',init:()=>({}),nodes:[{id:'one',dependencies:[],construct:()=>Effect.failCause(original)}],expose:s=>s},()=>Effect.sync(()=>programs++));
const exit=await Effect.runPromiseExit(__ef_catch(recipe,'ConfigError',()=>recovered++));
if(!Exit.isFailure(exit)||programs!==0||recovered!==0||exit.cause.reasons.length!==2||exit.cause.reasons.some(reason=>reason._tag!=='Fail'||reason.error!==error)||Cause.combine(exit.cause,exit.cause).reasons.length!==2)throw new Error('per-reason occurrence identity lost or composite failure recovered');
console.log('composite producer occurrences');
`)
	if output != "composite producer occurrences\n" {
		t.Fatal(output)
	}
}

func TestLayerJSTimeoutPreservesRetainedReasonOccurrences(t *testing.T) {
	output := runJS(t, layerApplicationSource+`
effect fn timed() -> string raises {Timeout} uses {Scheduler} { run main().timeout(1) }
`, `
const deferred=()=>{let resolve;const promise=new Promise(done=>resolve=done);return {promise,resolve};};
const traceKey=Context.Service('test/TimeoutOccurrenceTrace'),trace=Context.make(traceKey,'producer trace'),timerTrace=Context.make(traceKey,'timer trace');
const mismatches=[];
for(const kind of ['timer-success','timer-failure','work'])for(const fresh of [false,true])for(const first of [0,1])for(const caught of [false,true]){
 const ready=[deferred(),deferred()],evaluations=[deferred(),deferred()],release=[deferred(),deferred()],canceled=[deferred(),deferred()],completed=[deferred(),deferred()];
 const timerReady=deferred(),timerGate=deferred(),timerCanceled=deferred(),timerRelease=deferred();
 const shared={_tag:'ConfigError',detail:'same failure'},errors=fresh?[{...shared},{...shared}]:[shared,shared];
 const cleanup={message:'same cleanup'},defects=fresh?[{...cleanup},{...cleanup}]:[cleanup,cleanup];
 const timerError=fresh?{...shared}:shared,timerDefect={message:'timer protected cleanup'};
 const order=[];let failures=0,programs=0;
 const nodes=['a','b'].map((id,index)=>({id,dependencies:[],construct:()=>Effect.onExit(Effect.uninterruptibleMask(restore=>Effect.gen(function*(){
  yield* Effect.acquireRelease(Effect.void,()=>Effect.sync(()=>order.push(id)).pipe(Effect.flatMap(()=>Effect.failCause(Cause.annotate(Cause.die(defects[index]),trace)))));
  yield* Effect.exit(restore(Effect.promise(signal=>{signal.addEventListener('abort',()=>canceled[index].resolve(),{once:true});ready[index].resolve();return evaluations[index].promise;})));
  yield* Effect.promise(()=>release[index].promise);failures++;return yield* Effect.failCause(Cause.annotate(Cause.fail(errors[index]),trace));
 })),()=>Effect.sync(()=>completed[index].resolve()))}));
 const recipe=__ef_provideLayer({id:'timeout-occurrences',init:()=>({}),nodes,expose:s=>s},()=>Effect.sync(()=>programs++));
 const scheduler={sleep:()=>Effect.uninterruptibleMask(restore=>Effect.gen(function*(){
  yield* Effect.exit(restore(Effect.promise(signal=>{signal.addEventListener('abort',()=>timerCanceled.resolve(),{once:true});timerReady.resolve();return timerGate.promise;})));
  if(kind==='work'){yield* Effect.promise(()=>timerRelease.promise);return yield* Effect.failCause(Cause.annotate(Cause.die(timerDefect),timerTrace));}
  if(kind==='timer-failure')return yield* Effect.failCause(Cause.annotate(Cause.fail(timerError),timerTrace));
 })),advance:()=>Effect.void,awaitRegistration:()=>Effect.void};
 const timed=__ef_timeout(recipe,1n);
 const result=Effect.runPromiseExit(Effect.provideService(caught?__ef_catch(timed,'ConfigError',()=>{throw new Error('composite timeout recovered');}):timed,__ef_service_Scheduler,scheduler));
 let timer;const deadline=new Promise((_,reject)=>timer=setTimeout(()=>reject(new Error('timeout occurrence control did not settle')),5000));
 try{
  await Promise.race([Promise.all([...ready.map(d=>d.promise),timerReady.promise]),deadline]);
  if(kind==='work'){
   evaluations[first].resolve();release[first].resolve();await Promise.race([canceled[1-first].promise,deadline]);release[1-first].resolve();
   await Promise.race([timerCanceled.promise,deadline]);timerRelease.resolve();
  }else{
   timerGate.resolve();await Promise.race([Promise.all(canceled.map(d=>d.promise)),deadline]);
   release[first].resolve();await Promise.race([completed[first].promise,deadline]);release[1-first].resolve();
  }
  const exit=await Promise.race([result,deadline]),reasons=Exit.isFailure(exit)?exit.cause.reasons:[];
  const expected=kind==='work'?['Fail','Fail','Die','Die','Die']:['Fail','Fail','Fail','Die','Die'];
  if(failures!==2||programs!==0||JSON.stringify(order)!=='["b","a"]'||JSON.stringify(reasons.map(r=>r._tag))!==JSON.stringify(expected)){mismatches.push({kind,fresh,first,failures,programs,order,tags:reasons.map(r=>r._tag)});continue;}
  const base=kind==='work'?0:1;
  if(reasons[base].error!==errors[0]||reasons[base+1].error!==errors[1]||reasons[base+2].defect!==defects[1]||reasons[base+3].defect!==defects[0])throw new Error('timeout altered canonical original payloads');
  if(reasons.slice(base,base+4).some(reason=>reason.annotations.get(traceKey.key)!=='producer trace')){mismatches.push({kind,fresh,first,trace:'producer annotations lost'});continue;}
  const timerReason=kind==='work'?reasons[4]:reasons[0];
  if(kind==='timer-success'){if(timerReason.error._tag!=='Timeout')throw new Error('timeout lost primary control');}
  else if(timerReason.annotations.get(traceKey.key)!=='timer trace'||(kind==='work'?timerReason.defect!==timerDefect:timerReason.error!==timerError))mismatches.push({kind,fresh,first,trace:'timer annotations or original payload lost'});
 }finally{clearTimeout(timer);}
}
if(mismatches.length)throw new Error('timeout discarded retained occurrences '+JSON.stringify(mismatches));
console.log('timeout producer occurrences');
`)
	if output != "timeout producer occurrences\n" {
		t.Fatal(output)
	}
}

func TestLayerJSPreservesEqualProducerAndCleanupOccurrences(t *testing.T) {
	output := runJS(t, layerApplicationSource, `
const deferred=()=>{let resolve;const promise=new Promise(done=>resolve=done);return {promise,resolve};};
const mismatches=[];
const traceKey=Context.Service('test/LayerOccurrenceTrace'),trace=Context.make(traceKey,'retained trace');
for(const fresh of [false,true])for(const first of [0,1])for(const interrupt of [false,true])for(const caught of [0,1,2]){
 const ready=[deferred(),deferred()],evaluations=[deferred(),deferred()],release=[deferred(),deferred()],canceled=[deferred(),deferred()],completed=[deferred(),deferred()];
 const shared={_tag:'ConfigError',detail:'same failure'},errors=fresh?[{...shared},{...shared}]:[shared,shared];
 const cleanup={message:'same cleanup'},defects=fresh?[{...cleanup},{...cleanup}]:[cleanup,cleanup];
 const order=[];let failures=0,programs=0,recovered=0;
 const nodes=['a','b'].map((id,index)=>({id,dependencies:[],construct:()=>Effect.onExit(Effect.uninterruptibleMask(restore=>Effect.gen(function*(){
  yield* Effect.acquireRelease(Effect.void,()=>Effect.sync(()=>order.push(id)).pipe(Effect.flatMap(()=>Effect.failCause(Cause.annotate(Cause.die(defects[index]),trace)))));
  yield* Effect.exit(restore(Effect.promise(signal=>{signal.addEventListener('abort',()=>canceled[index].resolve(),{once:true});ready[index].resolve();return evaluations[index].promise;})));
  yield* Effect.promise(()=>release[index].promise);failures++;return yield* Effect.failCause(Cause.annotate(Cause.fail(errors[index]),trace));
 })),()=>Effect.sync(()=>completed[index].resolve()))}));
 const recipe=__ef_provideLayer({id:'equal-occurrences',init:()=>({}),nodes,expose:s=>s},()=>Effect.sync(()=>programs++));
 const once=()=>__ef_catch(recipe,'ConfigError',()=>{recovered++;});
 const program=caught===2?__ef_catch(once(),'ConfigError',()=>{recovered++;}):caught===1?once():recipe;
 const fiber=Effect.runFork(program),result=Effect.runPromise(Fiber.await(fiber));
 let timer;const deadline=new Promise((_,reject)=>timer=setTimeout(()=>reject(new Error('equal producer control did not settle')),5000));
 try{
  await Promise.race([Promise.all(ready.map(d=>d.promise)),deadline]);
  if(interrupt){
   fiber.interruptUnsafe();await Promise.race([Promise.all(canceled.map(d=>d.promise)),deadline]);
   release[first].resolve();await Promise.race([completed[first].promise,deadline]);release[1-first].resolve();
  }else{
   evaluations[first].resolve();release[first].resolve();
   await Promise.race([canceled[1-first].promise,deadline]);release[1-first].resolve();
  }
  const exit=await Promise.race([result,deadline]),reasons=Exit.isFailure(exit)?exit.cause.reasons:[];
  const expectedTags=interrupt?['Interrupt','Fail','Fail','Die','Die']:['Fail','Fail','Die','Die'];
  const actualTags=reasons.map(r=>r._tag);
  if(failures!==2||programs!==0||recovered!==0||JSON.stringify(order)!=='["b","a"]'||JSON.stringify(actualTags)!==JSON.stringify(expectedTags)){
   mismatches.push({fresh,first,interrupt,caught,failures,programs,recovered,order,actualTags});continue;
  }
  const base=interrupt?1:0;
  if(reasons[base].error!==errors[0]||reasons[base+1].error!==errors[1]||reasons[base+2].defect!==defects[1]||reasons[base+3].defect!==defects[0])throw new Error('original payload identity or canonical occurrence order changed');
  if(reasons.slice(base).some(reason=>reason.annotations.get(traceKey.key)!=='retained trace'))throw new Error('original annotations changed');
  // Recombination is a real Effect operation and must retain distinct producer
  // occurrences while deduplicating the same already-published occurrence.
  const recombined=Cause.combine(exit.cause,exit.cause);
  if(recombined.reasons.length!==reasons.length)throw new Error('occurrence identity does not survive recombination');
 }finally{clearTimeout(timer);}
}

if(mismatches.length)throw new Error('lost producer occurrences '+JSON.stringify(mismatches));
console.log('equal producer occurrences');
`)
	if output != "equal producer occurrences\n" {
		t.Fatal(output)
	}
}

func TestLayerJSCatchObservesProtectedExitAndPreservesCancellation(t *testing.T) {
	output := runJS(t, layerApplicationSource, `
const traceKey=Context.Service('test/CatchTrace'),trace=Context.make(traceKey,'catch trace');
const failure={_tag:'ConfigError',detail:'same payload'},defect={message:'fallback defect'};
let recoveries=0;
const recover=()=>{recoveries++;return 'recovered';};
const success=await Effect.runPromiseExit(__ef_catch(Effect.succeed('original'),'ConfigError',recover));
if(!Exit.isSuccess(success)||success.value!=='original'||recoveries!==0)throw new Error('success incorrectly recovered');
const matched=await Effect.runPromiseExit(__ef_catch(Effect.fail(failure),'ConfigError',recover));
if(!Exit.isSuccess(matched)||matched.value!=='recovered'||recoveries!==1)throw new Error('singleton matching failure not recovered');
const mismatch=await Effect.runPromiseExit(__ef_catch(Effect.failCause(Cause.annotate(Cause.fail(failure),trace)),'Other',recover));
if(!Exit.isFailure(mismatch)||mismatch.cause.reasons.length!==1||mismatch.cause.reasons[0].error!==failure||mismatch.cause.reasons[0].annotations.get(traceKey.key)!=='catch trace'||recoveries!==1)throw new Error('mismatched failure changed');
const thrown=await Effect.runPromiseExit(__ef_catch(Effect.fail(failure),'ConfigError',()=>{throw defect;}));
if(!Exit.isFailure(thrown)||thrown.cause.reasons.length!==1||thrown.cause.reasons[0].defect!==defect)throw new Error('thrown fallback not a retained defect');
const nested=await Effect.runPromiseExit(__ef_catch(__ef_catch(Effect.fail(failure),'Other',recover),'ConfigError',recover));
if(!Exit.isSuccess(nested)||nested.value!=='recovered'||recoveries!==2)throw new Error('nested singleton catch changed');
// Cancellation requested by the fallback itself occurs after protected Exit
// capture and the singleton decision. Delivery still observes it.
let recoveredAfterCapture=0;
const canceled=await Effect.runPromiseExit(Effect.withFiber(fiber=>__ef_catch(Effect.fail(failure),'ConfigError',()=>{recoveredAfterCapture++;fiber.interruptUnsafe();return 'canceled fallback';})));
if(!Exit.isFailure(canceled)||!Cause.hasInterruptsOnly(canceled.cause)||recoveredAfterCapture!==1)throw new Error('fallback delivery swallowed cancellation');
console.log('protected catch controls');
`)
	if output != "protected catch controls\n" {
		t.Fatal(output)
	}
}
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
