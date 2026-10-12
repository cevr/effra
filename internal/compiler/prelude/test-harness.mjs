const __ef_makeTestHarness=()=>{
  let now=0;
  let sequence=0;
  const timers=[];
  const invalidDuration=()=>Effect.die(new Error('invalid millisecond duration'));
  const remove=(timer)=>{const index=timers.indexOf(timer);if(index>=0)timers.splice(index,1);};
  // Every managed fiber runs on this dispatcher: a fork, a yield and a
  // preempted continuation are queued tasks. A callback resume (a latch
  // signal, a fired timer, an interruption, a fiber's completion) instead runs
  // its fiber inline, on top of the resumer's still-running turn. An empty
  // queue therefore says nothing while any fiber is mid-turn.
  //
  // Quiescence (Scheduler.awaitRegistration and each deadline selection of
  // Scheduler.advance) is decided only by the top-level drain, which runs from
  // a microtask, so no fiber's turn is on the stack beneath it. A quiescence
  // waiter suspends its fiber; the drain resumes one waiter only when the
  // queue is empty, then drains whatever that resume made runnable before it
  // considers the next waiter.
  const queued=[];const quiescenceWaiters=[];let drainScheduled=false;
  const scheduleDrain=()=>{if(!drainScheduled){drainScheduled=true;queueMicrotask(drain);}};
  const runQueued=()=>{while(queued.length>0){queued.shift().task();}};
  const drain=()=>{
    try{
      while(true){
        runQueued();
        const waiter=quiescenceWaiters.shift();
        if(waiter===undefined)break;
        waiter();
      }
    }finally{
      drainScheduled=false;
      if(queued.length>0||quiescenceWaiters.length>0)scheduleDrain();
    }
  };
  const dispatcher={
    scheduleTask:(task,priority)=>{let index=queued.length;for(let i=0;i<queued.length;i++){if(queued[i].priority>priority){index=i;break;}}queued.splice(index,0,{task,priority});scheduleDrain();},
    // Effect may flush synchronously from inside a fiber. That runs queued
    // tasks only; it never decides quiescence.
    flush:runQueued
  };
  const effectScheduler={executionMode:'sync',shouldYield:fiber=>fiber.currentOpCount>=2048,makeDispatcher:()=>dispatcher};
  // Returns once no managed fiber is running or runnable, so every timer that
  // managed work can register without outside input is registered. It is
  // level-triggered on that state, never on a new registration: forked work
  // that completed, or whose timer was cancelled or never started (Effect runs
  // no body of a fiber interrupted before its first turn), registers nothing
  // and still passes.
  const awaitQuiescence=Effect.callback((resume)=>{
    const waiter=()=>resume(Effect.void);
    quiescenceWaiters.push(waiter);
    scheduleDrain();
    return Effect.sync(()=>{const index=quiescenceWaiters.indexOf(waiter);if(index>=0)quiescenceWaiters.splice(index,1);});
  });
  const sleep=(duration)=>Effect.callback((resume,signal)=>{
    const millis=Duration.toMillis(duration);
    if(!Number.isFinite(millis)||millis<0||millis>2147483647){resume(invalidDuration());return;}
    if(millis===0){resume(Effect.succeed(undefined));return;}
    const timer={deadline:now+millis,sequence:sequence++,resume,cancelled:false};
    timer.abort=()=>{if(timer.cancelled)return;timer.cancelled=true;remove(timer);};
    timers.push(timer);
    timers.sort((left,right)=>left.deadline-right.deadline||left.sequence-right.sequence);
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
    return Effect.gen(function*(){
      if(now>Number.MAX_SAFE_INTEGER-ms)return yield* Effect.die(new Error('test scheduler time overflow'));
      const target=now+ms;
      while(true){
        // Each selection waits for quiescence, so every managed continuation,
        // including one resumed inline by the controller's caller or by the
        // timers fired below, has registered its next wait.
        yield* awaitQuiescence;
        const next=timers[0];
        if(next===undefined||next.deadline>target){now=target;return;}
        now=next.deadline;
        const due=[];
        while(timers[0]!==undefined&&timers[0].deadline<=now)due.push(timers.shift());
        for(const timer of due){if(timer.cancelled)continue;timer.cancelled=true;timer.resume(Effect.succeed(undefined));}
      }
    });
  };
  return{clock:{sleep:ms=>{if(ms<0n||ms>2147483647n)return invalidDuration();return Effect.sleep(Number(ms));}},effectClock,effectScheduler,scheduler:{sleep:ms=>{if(ms<0n||ms>2147483647n)return invalidDuration();return Effect.sleep(Number(ms));},advance:ms=>adjust(Number(ms)),awaitRegistration:()=>awaitQuiescence},sync:__ef_provider_TestSync};
};
