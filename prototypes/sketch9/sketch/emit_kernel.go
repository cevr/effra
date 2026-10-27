package sketch

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Kernel mode links the merged kernel runtime (effra.local/prototype/runtime/rt)
// and makes the program traceable by construction. Every definition ID it
// names comes from the Model, which the checker's declarations determine; the
// emitter writes them as constants and never computes one at run time.
//
//   - Task events (start, body-exit, cancel-request, observe, deadline,
//     release, panic) come from the runtime, keyed by the task's span. The
//     root's span is <package>.main and a timeout block's span is its block
//     ID, so the runtime needs no site: sites__ maps each task definition to
//     its declaration site.
//   - Point events (call, call-exit, interrupt, construct, handler) are
//     raised by the emitted code at their own site, through Task.TraceCall,
//     Task.TraceCallExit and Task.Trace. A capability call holds its
//     occurrence ID in a local and passes it to its call-exit with the call's
//     error, so the outcome is recorded, not inferred; the runtime links a
//     handler to the occurrence it caught. With no tracer each costs one load
//     and compare.
//   - main reads EFFRA_TRACE. Unset, the root has no tracer and the program
//     writes nothing; set, a bounded recorder collects the run and main writes
//     it as JSON Lines when the root task ends.

// kernelMain writes main for a kernel program.
func (e *emitter) kernelMain() {
	root := e.def(e.model.id("main"))
	e.line("func main() {")
	e.depth++
	e.line("var t__ *rt.Task")
	e.line("var rec__ *rttrace.Recorder")
	e.line("path__ := os.Getenv(\"EFFRA_TRACE\")")
	e.line("if path__ != \"\" {")
	e.line("\trec__ = rttrace.New(rttrace.DefaultCapacity)")
	e.line("\tt__ = rt.TracedRoot(%s, rec__)", root)
	e.line("} else {")
	e.line("\tt__ = rt.Root(%s)", root)
	e.line("}")
	e.line("_, err__ := rt.Run(t__, func(t__ *rt.Task) (struct{}, error) {")
	e.line("\tmain__(&Env__{Console: Stdout__{}}, t__)")
	e.line("\treturn struct{}{}, nil")
	e.line("})")
	e.line("if rec__ != nil {")
	e.line("\tif werr__ := rttrace.WriteFile(path__, rec__, revision__, \"sketchc -rt kernel\", sites__); werr__ != nil {")
	e.line("\t\tfmt.Fprintln(os.Stderr, werr__)")
	e.line("\t}")
	e.line("}")
	e.line("if code__ := rt.ExitCode(err__); code__ != 0 {")
	e.line("\tif code__ != 130 {")
	e.line("\t\tfmt.Fprintln(os.Stderr, err__)")
	e.line("\t}")
	e.line("\tos.Exit(code__)")
	e.line("}")
	e.depth--
	e.line("}")
	e.line("")
	e.line("// Console__ is the host Console capability; Stdout__ provides it.")
	e.line("type Console__ interface{ Println(t *rt.Task, args ...any) }")
	e.line("")
	e.line("type Stdout__ struct{}")
	e.line("")
	e.line("func (Stdout__) Println(_ *rt.Task, args ...any) { fmt.Println(args...) }")
	e.line("")
}

// def returns the Go constant for a definition ID, declaring it on first use.
func (e *emitter) def(id string) string {
	if c, ok := e.consts[id]; ok {
		return c
	}
	c := fmt.Sprintf("def__%d", len(e.constIDs)+1)
	e.consts[id] = c
	e.constIDs = append(e.constIDs, id)
	return c
}

// kernelTables returns the revision, the definition ID constants and the site
// table of the task definitions.
func (e *emitter) kernelTables() string {
	var b strings.Builder
	fmt.Fprintf(&b, "// revision__ is the source revision of %s (SHA-256).\nconst revision__ = %s\n\n", e.src, strconv.Quote(e.revision))
	b.WriteString("// Definition IDs of the checked model, derived from its declarations.\nconst (\n")
	for _, id := range e.constIDs {
		d, _ := e.model.Def(id)
		fmt.Fprintf(&b, "\t%s = %s // %s", e.consts[id], strconv.Quote(id), d.Kind)
		if d.Site != "" {
			fmt.Fprintf(&b, " at %s", d.Site)
		}
		b.WriteString("\n")
	}
	b.WriteString(")\n\n")
	b.WriteString("// sites__ maps each task definition to its declaration site, for the\n// events the runtime raises without one.\nvar sites__ = map[string]string{\n")
	var tasks []string
	for _, id := range e.constIDs {
		if d, _ := e.model.Def(id); d.Kind == DefTimeout || id == e.model.id("main") {
			tasks = append(tasks, id)
		}
	}
	sort.Strings(tasks)
	for _, id := range tasks {
		d, _ := e.model.Def(id)
		fmt.Fprintf(&b, "\t%s: %s,\n", e.consts[id], strconv.Quote(d.Site))
	}
	b.WriteString("}\n\n")
	return b.String()
}

// traceAt writes a point event of kind for definition id at p: an
// interrupt, a construct or a handler. A capability call goes through
// callStart and callExit.
func (e *emitter) traceAt(kind, id string, p Pos) {
	if !e.kernel {
		return
	}
	k := map[string]string{"interrupt": "TraceInterrupt", "construct": "TraceConstruct", "handler": "TraceHandler"}[kind]
	e.line("t__.Trace(rt.%s, %s, %s)", k, e.def(id), strconv.Quote(e.model.site(p)))
}

// callStart writes the call event of capability operation recv.op at p and
// returns the variable that holds its occurrence ID, "" outside kernel mode.
func (e *emitter) callStart(recv, op string, p Pos) string {
	if !e.kernel {
		return ""
	}
	occ := e.temp("c")
	e.line("%s := t__.TraceCall(%s, %s)", occ, e.def(e.model.CapOpID(recv, op)), strconv.Quote(e.model.site(p)))
	return occ
}

// callExit writes the call-exit event of the call whose occurrence ID is in
// occ: its outcome is err, the call's error variable, or ok for a call that
// cannot fail.
func (e *emitter) callExit(occ, recv, op string, p Pos, err string) {
	if occ == "" {
		return
	}
	if err == "" {
		err = "nil"
	}
	e.line("t__.TraceCallExit(%s, %s, %s, %s)", occ, e.def(e.model.CapOpID(recv, op)), strconv.Quote(e.model.site(p)), err)
}

// sleep lowers Scheduler.Sleep(d) to rt.Sleep, which returns Interrupted when
// the task is stopped while it waits. Both runtimes provide rt.Sleep with
// this signature. In kernel mode the sleep is a traced capability call whose
// outcome is ok or interrupted.
func (e *emitter) sleep(d, recv, op string, site Pos) {
	err := e.temp("e")
	if !e.kernel {
		e.line("if %s := rt.Sleep(t__, %s.Scheduler, %s); %s != nil {", err, e.envVal, d, err)
		e.depth++
		e.fail(err)
		e.depth--
		e.line("}")
		return
	}
	occ := e.callStart(recv, op, site)
	e.line("%s := rt.Sleep(t__, %s.Scheduler, %s)", err, e.envVal, d)
	e.callExit(occ, recv, op, site, err)
	e.line("if %s != nil {", err)
	e.depth++
	e.traceAt("interrupt", e.enclosing(), site)
	e.fail(err)
	e.depth--
	e.line("}")
}

// handlerEvents writes the handler event of a catch arm: the error it
// handled, at the arm. An arm with several types switches once more to name
// the one that matched.
func (e *emitter) handlerEvents(err string, arm CatchArm) {
	if !e.kernel {
		return
	}
	if len(arm.Types) == 1 {
		e.traceAt("handler", e.model.ErrorID(arm.Types[0].String()), arm.Pos)
		return
	}
	e.line("switch %s.(type) {", err)
	for _, q := range arm.Types {
		e.line("case %s:", e.errorType(q.String()))
		e.depth++
		e.traceAt("handler", e.model.ErrorID(q.String()), arm.Pos)
		e.depth--
	}
	e.line("}")
}

// enclosing is the innermost enclosing definition: a function or a timeout
// block.
func (e *emitter) enclosing() string { return e.in[len(e.in)-1] }
