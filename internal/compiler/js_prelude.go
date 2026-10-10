package compiler

import (
	"embed"
	"fmt"
	"slices"
	"strings"
)

//go:embed prelude/*.mjs
var jsPreludeSources embed.FS

// jsEffectImports is the canonical order of the `effect` named imports a
// generated module may use. A module imports only the names its emitted
// chunks and declarations declare.
var jsEffectImports = []string{"Context", "Clock", "Duration", "Effect", "Fiber", "Scope", "Scheduler", "Exit", "Cause", "Option", "Queue"}

// jsPreludeChunk is one fixed piece of generated JavaScript: a lowering
// helper, a builtin provider implementation or the test harness. It names
// the chunks it calls and the `effect` imports it uses, so selection closes
// over declared edges instead of scanning emitted text.
type jsPreludeChunk struct {
	name     string
	requires []string
	imports  []string
	// hostModules are the host modules the chunk loads on demand. The
	// prelude renders one loader per module (jsHostLoader), and the chunk
	// calls the loader instead of spelling an import.
	hostModules []string
	source      string
}

func preludeFile(name string) string {
	data, err := jsPreludeSources.ReadFile("prelude/" + name)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// jsPrelude lists every chunk in emission order; a chunk follows the chunks
// it requires.
var jsPrelude = []jsPreludeChunk{
	{name: "lifecycle", imports: []string{"Cause", "Context", "Effect", "Exit", "Fiber", "Option", "Scope"}, source: preludeFile("lifecycle.mjs")},
	{name: "fork", requires: []string{"lifecycle"}, imports: []string{"Effect"}, source: preludeFile("fork.mjs")},
	{name: "child", requires: []string{"lifecycle"}, imports: []string{"Effect"}, source: preludeFile("child.mjs")},
	{name: "fiber.join", requires: []string{"child"}, imports: []string{"Effect", "Fiber"}, source: preludeFile("join.mjs")},
	{name: "fiber.cancel", imports: []string{"Effect"}, source: preludeFile("cancel.mjs")},
	{name: "fiber.interrupt", requires: []string{"child", "fiber.cancel"}, imports: []string{"Cause", "Effect", "Exit", "Fiber"}, source: preludeFile("interrupt.mjs")},
	{name: "call", imports: []string{"Effect"}, source: "const __ef_call = (service, method, args) => Effect.flatMap(service, provider => provider[method](...args));\n"},
	{name: "timeout", requires: []string{"lifecycle", "fork", "call"}, imports: []string{"Cause", "Effect", "Exit", "Fiber"}, source: preludeFile("timeout.mjs")},
	{name: "recover", imports: []string{"Effect", "Exit"}, source: preludeFile("recover.mjs")},
	{name: "catch", requires: []string{"recover"}, imports: []string{"Effect"}, source: preludeFile("catch.mjs")},
	{name: "layers", requires: []string{"lifecycle"}, imports: []string{"Cause", "Effect", "Exit", "Fiber", "Queue"}, source: preludeFile("layers.mjs")},
	{name: "provider:LiveHttp", requires: []string{"lifecycle", "quote"}, imports: []string{"Cause", "Effect", "Exit"}, hostModules: []string{"node:dns/promises", "node:http", "node:net"}, source: preludeFile("http.mjs")},
	{name: "codec", imports: []string{"Effect"}, source: codecEngineJS + jsCodecAdapters},
	{name: i64FormatOp, source: jsI64FormatHelper},
	{name: i64ParseOp, imports: []string{"Effect"}, source: jsI64ParseHelper},
	{name: "quote", source: preludeFile("quote.mjs")},
	{name: "entry", requires: []string{"quote"}, imports: []string{"Effect", "Exit"}, source: preludeFile("entry.mjs")},
	{name: "harness-slot", source: "let __ef_test_harness=null;\n"},
	{name: "latch", imports: []string{"Effect"}, source: preludeFile("latch.mjs")},
	{name: "provider:Stdout", imports: []string{"Effect"}, source: "const __ef_provider_Stdout = { log: (message) => Effect.sync(() => console.log(message)) };\n"},
	{name: "provider:Assertions", requires: []string{"quote"}, imports: []string{"Effect"}, source: "const __ef_provider_Assertions={check:(condition,message)=>condition?Effect.succeed(undefined):Effect.fail({_tag:'AssertionFailed',message}),equalText:(actual,expected)=>actual===expected?Effect.succeed(undefined):Effect.fail({_tag:'AssertionFailed',message:'expected '+__ef_quoteText(expected)+'; received '+__ef_quoteText(actual)})};\n"},
	{name: "provider:LiveClock", imports: []string{"Effect"}, source: "const __ef_provider_LiveClock={sleep:ms=> ms<0n || ms>2147483647n ? Effect.die(new Error('invalid millisecond duration')) : Effect.callback((resume,signal)=>{const timer=setTimeout(()=>resume(Effect.succeed(undefined)),Number(ms));const abort=()=>{clearTimeout(timer);resume(Effect.interrupt);};signal.addEventListener('abort',abort,{once:true});return Effect.sync(()=>{clearTimeout(timer);signal.removeEventListener('abort',abort);});})};\n"},
	{name: "provider:TestClock", requires: []string{"harness-slot"}, imports: []string{"Effect"}, source: "const __ef_provider_TestClock={sleep:ms=>__ef_test_harness?__ef_test_harness.clock.sleep(ms):Effect.die(new Error('test clock requires the ef test harness'))};\n"},
	{name: "provider:LiveEnv", imports: []string{"Effect"}, source: "const __ef_provider_LiveEnv={get:name=>Effect.sync(()=>process.env[name] ?? '')};\n"},
	{name: "provider:LiveScheduler", requires: []string{"provider:LiveClock"}, imports: []string{"Effect"}, source: "const __ef_provider_LiveScheduler={sleep:ms=>__ef_provider_LiveClock.sleep(ms),advance:()=>Effect.die(new Error('live scheduler cannot advance')),awaitRegistration:()=>Effect.die(new Error('live scheduler has no registration barrier'))};\n"},
	{name: "provider:TestScheduler", requires: []string{"harness-slot"}, imports: []string{"Effect"}, source: "const __ef_provider_TestScheduler={sleep:ms=>__ef_test_harness?__ef_test_harness.scheduler.sleep(ms):Effect.die(new Error('test scheduler requires the ef test harness')),advance:ms=>__ef_test_harness?__ef_test_harness.scheduler.advance(ms):Effect.die(new Error('test scheduler adjustment is only available in the ef test harness')),awaitRegistration:()=>__ef_test_harness?__ef_test_harness.scheduler.awaitRegistration():Effect.die(new Error('test scheduler registration barrier is only available in the ef test harness'))};\n"},
	{name: "provider:TestSync", requires: []string{"latch"}, imports: []string{"Effect"}, source: "const __ef_provider_TestSync={latch:()=>Effect.sync(()=>new __ef_latch()),await:latch=>latch?.await?.()??Effect.die(new Error('invalid latch handle')),signal:latch=>latch?.signal?.()??Effect.die(new Error('invalid latch handle'))};\n"},
	{name: "test-harness", requires: []string{"harness-slot", "provider:Assertions", "provider:TestSync"}, imports: []string{"Cause", "Clock", "Duration", "Effect", "Exit", "Scheduler"}, source: preludeFile("test-harness.mjs")},
}

// jsPreludeChunkFor maps a plan's lowering-helper identity to the chunk
// that implements it in JavaScript. Helpers without a JavaScript lowering
// belong to Go-only features, which refuse JavaScript before planning.
var jsPreludeChunkFor = map[string]string{
	"scope":           "lifecycle",
	"fork":            "fork",
	"fiber.join":      "fiber.join",
	"fiber.cancel":    "fiber.cancel",
	"fiber.interrupt": "fiber.interrupt",
	"timeout":         "timeout",
	"catch":           "catch",
	"recover":         "recover",
	"codec":           "codec",
	i64FormatOp:       i64FormatOp,
	i64ParseOp:        i64ParseOp,
}

// jsSelection is the set of chunks and `effect` imports one module emits.
type jsSelection struct {
	chunks  map[string]bool
	imports map[string]bool
}

func newJSSelection() *jsSelection {
	return &jsSelection{chunks: map[string]bool{}, imports: map[string]bool{}}
}

// use selects a chunk and, transitively, the chunks it requires.
func (s *jsSelection) use(name string) error {
	if s.chunks[name] {
		return nil
	}
	index := slices.IndexFunc(jsPrelude, func(chunk jsPreludeChunk) bool { return chunk.name == name })
	if index < 0 {
		return fmt.Errorf("JavaScript has no lowering for %s", name)
	}
	s.chunks[name] = true
	for _, dependency := range jsPrelude[index].requires {
		if err := s.use(dependency); err != nil {
			return err
		}
	}
	return nil
}

func (s *jsSelection) importEffect(names ...string) {
	for _, name := range names {
		s.imports[name] = true
	}
}

// effectNames closes the module's `effect` imports over its selected chunks
// and returns them in canonical order.
func (s *jsSelection) effectNames() []string {
	for _, chunk := range jsPrelude {
		if s.chunks[chunk.name] {
			s.importEffect(chunk.imports...)
		}
	}
	names := []string{}
	for _, name := range jsEffectImports {
		if s.imports[name] {
			names = append(names, name)
		}
	}
	return names
}

// moduleImports is the module's complete import set: the static `effect`
// import the prelude renders and every selected chunk's dynamic host module.
// Call it after the module's declarations are lowered, as their `effect`
// names belong to the set.
func (s *jsSelection) moduleImports() []JSImport {
	imports := []JSImport{}
	if names := s.effectNames(); len(names) > 0 {
		imports = append(imports, JSImport{Specifier: "effect", Names: names})
	}
	for _, host := range s.hostModules() {
		imports = append(imports, JSImport{Specifier: host, Names: []string{}, Dynamic: true})
	}
	return imports
}

// hostModules lists, sorted, the host modules the selected chunks declare.
func (s *jsSelection) hostModules() []string {
	hosts := []string{}
	for _, chunk := range jsPrelude {
		if s.chunks[chunk.name] {
			for _, host := range chunk.hostModules {
				if !slices.Contains(hosts, host) {
					hosts = append(hosts, host)
				}
			}
		}
	}
	slices.Sort(hosts)
	return hosts
}

// jsHostLoader names the function that loads one host module. Chunk text
// calls the loader and never spells an import, so the prelude renders every
// host import from a chunk's declaration and an undeclared import cannot be
// emitted.
func jsHostLoader(specifier string) string {
	name := []byte("__ef_host_")
	for _, r := range []byte(specifier) {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			name = append(name, r)
		} else {
			name = append(name, '_')
		}
	}
	return string(name)
}

// prelude renders the `effect` import declaration followed by the selected
// chunks in table order.
func (s *jsSelection) prelude() string {
	names := s.effectNames()
	out := ""
	if len(names) > 0 {
		out = "import { " + strings.Join(names, ", ") + " } from 'effect';\n"
	}
	for _, host := range s.hostModules() {
		out += "const " + jsHostLoader(host) + " = () => import('" + host + "');\n"
	}
	for _, chunk := range jsPrelude {
		if s.chunks[chunk.name] {
			out += chunk.source
		}
	}
	return out
}
