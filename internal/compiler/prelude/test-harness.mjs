const __ef_makeTestHarness=()=>{
  let now=0;
  let sequence=0;
  let registrations=0;
  let observed=0;
  const registrationWaiters=new Set();
  const timers=[];
  const invalidDuration=()=>Effect.die(new Error('invalid millisecond duration'));
  const remove=(timer)=>{const index=timers.indexOf(timer);if(index>=0)timers.splice(index,1);};
  const register=()=>{registrations++;const waiters=[...registrationWaiters];registrationWaiters.clear();for(const wake of waiters)wake();};
  const queued=[];let flushScheduled=false;
  const dispatcher={
    scheduleTask:(task,priority)=>{let index=queued.length;for(let i=0;i<queued.length;i++){if(queued[i].priority>priority){index=i;break;}}queued.splice(index,0,{task,priority});if(!flushScheduled){flushScheduled=true;queueMicrotask(()=>{flushScheduled=false;dispatcher.flush();});}},
    flush:()=>{flushScheduled=false;while(queued.length>0){queued.shift().task();}}
  };
  const effectScheduler={executionMode:'sync',shouldYield:fiber=>fiber.currentOpCount>=2048,makeDispatcher:()=>dispatcher};
  const flush=Effect.sync(()=>dispatcher.flush());
  const awaitRegistration=Effect.callback((resume,signal)=>{
    if(registrations>observed){observed=registrations;resume(Effect.succeed(undefined));return;}
    const wake=()=>{observed=registrations;resume(Effect.succeed(undefined));};
    registrationWaiters.add(wake);
    signal.addEventListener('abort',()=>registrationWaiters.delete(wake),{once:true});
    return Effect.sync(()=>registrationWaiters.delete(wake));
  });
  const awaitReady=Effect.gen(function*(){
    while(true){
      yield* flush;
      const before=registrations;
      if(before===observed)yield* awaitRegistration;else observed=before;
      yield* flush;
      if(registrations===observed)return;
      observed=registrations;
    }
  });
  const sleep=(duration)=>Effect.callback((resume,signal)=>{
    const millis=Duration.toMillis(duration);
    if(!Number.isFinite(millis)||millis<0||millis>2147483647){resume(invalidDuration());return;}
    if(millis===0){resume(Effect.succeed(undefined));return;}
    const timer={deadline:now+millis,sequence:sequence++,resume,cancelled:false};
    timer.abort=()=>{if(timer.cancelled)return;timer.cancelled=true;remove(timer);};
    timers.push(timer);
    timers.sort((left,right)=>left.deadline-right.deadline||left.sequence-right.sequence);
    register();
    signal.addEventListener('abort',timer.abort,{once:true});
    return Effect.sync(()=>{timer.abort();signal.removeEventListener('abort',timer.abort);});
  });
  const effectClock={
    currentTimeMillisUnsafe:()=>now,
    currentTimeMillis:Effect.sync(()=>now),
    currentTimeNanosUnsafe:()=>BigInt(now)*1000000n,
    currentTimeNanos:Effect.sync(()=>BigInt(now)*1000000n),
    monotonicTimeNanosUnsafe:()=>BigInt(now)*1000000n,
    monotonicTimeNanos:Effect.sync(()=>BigInt(now)*1000000n),
    sleep
  };
  const adjust=(ms)=>{
    if(!Number.isSafeInteger(ms)||ms<0||ms>2147483647)return invalidDuration();
    if(now>Number.MAX_SAFE_INTEGER-ms)return Effect.die(new Error('test scheduler time overflow'));
    const target=now+ms;
    return Effect.gen(function*(){
      // Admit queued managed fibers before observing the first deadline. The
      // dispatcher is the JS equivalent of the Go scheduler's admission
      // reservation; without this flush an unstarted fork can register its
      // timer after the controller has committed the target.
      yield* flush;
      observed=registrations;
      while(true){
        // Each selection is made only after queued managed continuations have
        // had a turn to register their next wait.
        yield* flush;
        const next=timers[0];
        if(next===undefined||next.deadline>target){
          // A flush can itself enqueue a timer. Recheck before committing the
          // target so a managed continuation cannot miss an in-range wake.
          yield* flush;
          const afterFlush=timers[0];
          if(afterFlush!==undefined&&afterFlush.deadline<=target)continue;
          now=target;return;
        }
        now=next.deadline;
        const due=[];
        while(timers[0]!==undefined&&timers[0].deadline<=now)due.push(timers.shift());
        for(const timer of due){if(timer.cancelled)continue;timer.cancelled=true;timer.resume(Effect.succeed(undefined));}
        yield* flush;
      }
    });
  };
  return{clock:{sleep:ms=>{if(ms<0n||ms>2147483647n)return invalidDuration();return Effect.sleep(Number(ms));}},effectClock,effectScheduler,scheduler:{sleep:ms=>{if(ms<0n||ms>2147483647n)return invalidDuration();return Effect.sleep(Number(ms));},advance:ms=>adjust(Number(ms)),awaitRegistration:()=>awaitReady},sync:__ef_provider_TestSync};
};
