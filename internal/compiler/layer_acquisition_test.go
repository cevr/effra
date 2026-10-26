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
}
