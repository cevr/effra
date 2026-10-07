class __ef_latch {
  constructor(){this.done=false;this.waiters=new Set();}
  await(){return Effect.callback((resume,signal)=>{if(this.done){resume(Effect.succeed(undefined));return;}const waiter={resume};this.waiters.add(waiter);const abort=()=>this.waiters.delete(waiter);signal.addEventListener('abort',abort,{once:true});return Effect.sync(()=>{this.waiters.delete(waiter);signal.removeEventListener('abort',abort);});});}
  signal(){return Effect.sync(()=>{if(this.done)return;this.done=true;const waiters=[...this.waiters];this.waiters.clear();for(const waiter of waiters)waiter.resume(Effect.succeed(undefined));});}
}
