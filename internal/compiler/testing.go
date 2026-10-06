package compiler

import (
	"fmt"
	"strings"
)

// Tests are ordinary checked effects. Only assertions are supplied implicitly;
// fixture services are explicit so host access cannot appear through a preset.
func (r *Result) Tests() ([]*Symbol, error) {
	if !r.Checked {
		return nil, fmt.Errorf("tests require checked source")
	}
	tests := []*Symbol{}
	for i := range r.Symbols {
		s := &r.Symbols[i]
		if !strings.HasPrefix(s.Name, "test_") {
			continue
		}
		if !s.Contract.Effect || s.Contract.Success != "()" || len(s.Params) != 0 {
			return nil, fmt.Errorf("%s must be an effect function with no parameters returning ()", s.Name)
		}
		for _, req := range s.Contract.Services {
			if req != "Assert" && req != "Clock" && req != "Scheduler" && req != "Sync" {
				return nil, fmt.Errorf("%s requires %s; provide fixture services explicitly", s.Name, req)
			}
		}
		tests = append(tests, s)
	}
	if len(tests) == 0 {
		return nil, fmt.Errorf("no test_ functions found")
	}
	return tests, nil
}
func (r *Result) EmitGoTests() (string, error) {
	tests, err := r.Tests()
	if err != nil {
		return "", err
	}
	return r.emitGo(tests)
}
func (r *Result) EmitJSTests() (string, string, error) {
	tests, err := r.Tests()
	if err != nil {
		return "", "", err
	}
	js, decl, err := r.Emit(false)
	if err != nil {
		return "", "", err
	}
	js += "const __ef_tests = ["
	for _, test := range tests {
		js += "[" + quoted(test.Name) + ",__ef_function_" + test.Name + "],"
	}
	js += `];
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
const __ef_results=[];let __ef_passed=true;
for(const [name,program] of __ef_tests){const __ef_harness=__ef_makeTestHarness();__ef_test_harness=__ef_harness;let __ef_case=Effect.provideService(program(),__ef_service_Assert,__ef_provider_Assertions);__ef_case=Effect.provideService(__ef_case,__ef_service_Clock,__ef_harness.clock);__ef_case=Effect.provideService(__ef_case,__ef_service_Scheduler,__ef_harness.scheduler);__ef_case=Effect.provideService(__ef_case,__ef_service_Sync,__ef_harness.sync);const __ef_clocked=Effect.provideService(__ef_case,Clock.Clock,__ef_harness.effectClock);const __ef_scheduled=Effect.provideService(__ef_clocked,Scheduler.Scheduler,__ef_harness.effectScheduler);const exit=await Effect.runPromiseExit(__ef_scheduled);__ef_test_harness=null;const passed=Exit.isSuccess(exit);__ef_passed&&=passed;__ef_results.push(passed?{name,passed}:{name,passed,cause:Cause.pretty(exit.cause),reasons:exit.cause.reasons.map(reason=>reason._tag==="Fail"?{kind:"failure",tag:reason.error?._tag??"Unknown",message:reason.error?.message??String(reason.error)}:reason._tag==="Die"?{kind:"defect",message:String(reason.defect)}:{kind:"interrupt",message:String(reason.fiberId??"interrupted")})});}
console.log(JSON.stringify({schemaVersion:1,passed:__ef_passed,tests:__ef_results}));if(!__ef_passed)process.exitCode=1;
`
	return js, decl, nil
}

// This is a conservative file-level capability check, not an OS sandbox.
func (r *Result) TestMode(live bool) error {
	if !r.Checked {
		return fmt.Errorf("tests require checked source")
	}
	if live {
		return nil
	}
	if r.Program.GoOnly {
		return fmt.Errorf("native host capabilities require test --live")
	}
	var block func(*Block) bool
	var expr func(*Expr) bool
	expr = func(e *Expr) bool {
		if e == nil {
			return false
		}
		if e.Kind == "name" && e.Text == "provider" && (e.Name == "LiveClock" || e.Name == "LiveScheduler" || e.Name == "LiveEnv") {
			return true
		}
		if expr(e.Left) || expr(e.Right) || block(e.Then) || block(e.Else) {
			return true
		}
		for _, a := range e.Args {
			if expr(a) {
				return true
			}
		}
		for _, field := range e.Fields {
			if expr(field.Value) {
				return true
			}
		}
		for _, arm := range e.Arms {
			if block(arm.Body) {
				return true
			}
		}
		return false
	}
	block = func(b *Block) bool {
		if b != nil {
			for _, s := range b.Statements {
				if expr(s.Value) {
					return true
				}
				if expr(s.Payload) {
					return true
				}
			}
		}
		return false
	}
	for _, f := range r.Program.Functions {
		if block(f.Body) {
			return fmt.Errorf("live clock/scheduler/environment requires test --live")
		}
	}
	for _, p := range r.Program.Providers {
		for _, f := range p.Methods {
			if block(f.Body) {
				return fmt.Errorf("live clock/scheduler/environment requires test --live")
			}
		}
	}
	return nil
}
