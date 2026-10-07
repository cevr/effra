package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const httpTransportRouteSource = `error Missing
service Users {effect fn get(path:string)->string raises {Missing}}
impl Memory for Users {effect fn get(path:string)->string raises {Missing}{path}}
effect fn route(request:HttpRequest)->HttpReply raises {Missing} uses {Users, Http} {
 let name = run Users.get(request.path)
 let body = run Http.text(name)
 HttpReply.Respond {response: HttpResponse {status: 200, contentType: request.contentType, body: body}}
}
fn limits()->HttpLimits {HttpLimits {maxBodyBytes: 16, readHeaderMillis: 1000, readBodyMillis: 1000, idleMillis: 1000, maxActive: 4}}
`

func TestHTTPListenRetainsHandlerRowsAndAbsorbedFailures(t *testing.T) {
	for _, target := range []string{"go", "js"} {
		source := httpTransportRouteSource + `effect fn main()->void{let pending=Http.listen("127.0.0.1:0",limits(),route).provide<Users>(Memory).provide<Http>(LiveHttp);void}`
		r := CompileFor(source, target)
		if !r.Checked {
			t.Fatalf("%s: %+v", target, r.Diagnostics)
		}
		info, err := r.TypeAt(strings.Index(source, "listen("))
		if err != nil {
			t.Fatal(err)
		}
		policy := info.Type.Application.CallbackPolicies
		if len(policy) != 1 || policy[0].Parameter != 2 || policy[0].Kind != "typed-failure-response" || !slices.Equal(policy[0].AbsorbedFailures, []string{"Missing"}) {
			t.Fatalf("%s: wrong transport policy: %+v", target, policy)
		}
		if !slices.Equal(info.Type.Errors, []string{"IoError"}) || !slices.Equal(info.Type.Services, []string{"Http", "Users"}) {
			t.Fatalf("%s: transport contract erased handler rows: %+v", target, info.Type)
		}
	}
	missing := CompileFor(httpTransportRouteSource+`effect fn main()->void raises {IoError}{run Http.listen("127.0.0.1:0",limits(),route).provide<Http>(LiveHttp)}`, "js")
	if missing.Checked || !hasCode(missing, "EF108") {
		t.Fatalf("handler service requirement was not propagated: %+v", missing.Diagnostics)
	}
}

func TestHTTPListenRejectsHandlerShapesAndShadowedContracts(t *testing.T) {
	for _, test := range []struct{ name, source, code string }{
		{"result", `effect fn route(request:HttpRequest)->string{request.path} effect fn main()->void raises {IoError}{run Http.listen("127.0.0.1:0",HttpLimits{maxBodyBytes:1,readHeaderMillis:1,readBodyMillis:1,idleMillis:1,maxActive:1},route).provide<Http>(LiveHttp)}`, "EF106"},
		{"parameter", `effect fn route(path:string)->HttpReply{HttpReply.NotFound {}} effect fn main()->void raises {IoError}{run Http.listen("127.0.0.1:0",HttpLimits{maxBodyBytes:1,readHeaderMillis:1,readBodyMillis:1,idleMillis:1,maxActive:1},route).provide<Http>(LiveHttp)}`, "EF106"},
		{"pure", `fn route(request:HttpRequest)->HttpReply{HttpReply.NotFound {}} effect fn main()->void raises {IoError}{run Http.listen("127.0.0.1:0",HttpLimits{maxBodyBytes:1,readHeaderMillis:1,readBodyMillis:1,idleMillis:1,maxActive:1},route).provide<Http>(LiveHttp)}`, "EF106"},
		{"limits", `effect fn route(request:HttpRequest)->HttpReply{HttpReply.NotFound {}} effect fn main()->void raises {IoError}{run Http.listen("127.0.0.1:0",16,route).provide<Http>(LiveHttp)}`, "EF106"},
		{"shadowed record", `record HttpRequest {path:string} effect fn main()->void{let pending=Http.text("x"); void}`, "EF101"},
		{"shadowed record by row", `record HttpRequest {path:string} effect fn label()->bytes uses {Http} {run Http.text("x")} effect fn main()->void{void}`, "EF101"},
		{"shadowed record by provider", `record HttpResponse {path:string} effect fn main()->void{let provider=LiveHttp; void}`, "EF101"},
		{"shadowed record by outer use", `record HttpRequest {path:string} effect fn main()->void{let pending=Http.text("x"); let Http="local"; void}`, "EF101"},
		{"shadowed callback", `record HttpHandler {path:string} effect fn main()->void{void}`, "EF101"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := Compile(test.source)
			if r.Checked || !hasCode(r, test.code) {
				t.Fatalf("expected %s: %+v", test.code, r.Diagnostics)
			}
		})
	}
}

// Admission follows lexically resolved references: a local binding named Http
// or LiveHttp is not a reference, so it neither reserves the builtin data
// names nor emits any transport.
func TestHTTPContractIsAdmittedOnlyWhenReferenced(t *testing.T) {
	for _, source := range []string{
		`record HttpRequest {path:string} effect fn main()->string{let label="Http"; label}`,
		`record HttpRequest { path: string }
effect fn main() -> string {
    let Http = "local"
    Http
}`,
		`record HttpRequest {path:string} fn echo(LiveHttp:string)->string{LiveHttp} effect fn main()->string{echo("x")}`,
		`record HttpRequest {path:string} enum Box {Item {Http: string}} fn open(box:Box)->string{match box {Box.Item {Http} => Http}} effect fn main()->string{open(Box.Item {Http: "x"})}`,
	} {
		for _, target := range []string{"go", "js"} {
			r := CompileFor(source, target)
			if !r.Checked {
				t.Fatalf("%s: %+v\n%s", target, r.Diagnostics, source)
			}
			for _, declaration := range r.Declarations {
				if declaration.Source == builtinDataSourceID {
					t.Fatalf("%s: unreferenced Http contract was admitted: %+v\n%s", target, r.Declarations, source)
				}
			}
			code := emitHTTPProgram(t, r, target)
			if strings.Contains(code, "efProvider_LiveHttp") || strings.Contains(code, "__ef_provider_LiveHttp") || strings.Contains(code, "efService_Http") || strings.Contains(code, "__ef_service_Http") || strings.Contains(code, "node:http") {
				t.Fatalf("%s: unreferenced Http contract was emitted\n%s", target, source)
			}
		}
	}
}

// The contract is admitted for its interface alone; the transport
// implementation is emitted only where checked code holds LiveHttp.
func TestHTTPImplementationIsEmittedOnlyForCheckedProviderReferences(t *testing.T) {
	library := `effect fn greeting()->bytes uses {Http} {run Http.text("hi")} `
	for _, test := range []struct {
		main        string
		implemented bool
	}{
		{`effect fn main()->void{void}`, false},
		{`effect fn main()->void raises {IoError}{let body=run greeting().provide<Http>(LiveHttp); void}`, true},
	} {
		for _, target := range []string{"go", "js"} {
			r := CompileFor(library+test.main, target)
			if !r.Checked {
				t.Fatalf("%s: %+v", target, r.Diagnostics)
			}
			code := emitHTTPProgram(t, r, target)
			implemented := strings.Contains(code, "efProvider_LiveHttp") || strings.Contains(code, "__ef_provider_LiveHttp")
			// The plan prunes the unreachable service, so only the implemented case
			// must name Http at all.
			if implemented != test.implemented || test.implemented && !strings.Contains(code, "Http") {
				t.Fatalf("%s: implementation emitted %v, want %v\n%s", target, implemented, test.implemented, test.main)
			}
			if target == "go" {
				buildGeneratedGo(t, r, GoGenerationBuild)
			}
		}
	}
}

func emitHTTPProgram(t *testing.T, r *Result, target string) string {
	t.Helper()
	if target == "go" {
		code, err := r.EmitGo()
		if err != nil {
			t.Fatal(err)
		}
		return code
	}
	code, _, err := r.Emit(true)
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func TestHTTPDataPreludeHasNoSourceSpans(t *testing.T) {
	r := Compile(`effect fn main()->void{let pending=Http.text("x"); void}`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	names := []string{}
	for _, declaration := range r.Declarations {
		if declaration.Source != builtinDataSourceID {
			continue
		}
		names = append(names, declaration.Name)
		if declaration.Span != (Span{}) {
			t.Fatalf("builtin declaration carries a source span: %+v", declaration)
		}
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"HttpLimits", "HttpReply", "HttpRequest", "HttpResponse"}) {
		t.Fatalf("builtin HTTP data: %v", names)
	}
}

// The JS transport is exercised through its provider with handler recipes
// whose finalizers are observable, proving publication after owned cleanup.
// Bun's node:http accepts a whole response at once and reports it finished,
// so response backpressure is observable, and its cases run, only on Node.
const httpTransportJSHarness = `
import net from 'node:net';
const backpressure = !process.versions.bun;
const limits = { maxBodyBytes: 16n, readHeaderMillis: 1000n, readBodyMillis: 1000n, idleMillis: 1000n, maxActive: 4n };
const deferred = () => { let resolve; const promise = new Promise(r => { resolve = r; }); return { promise, resolve }; };
const pending = (promise, ms = 100) => Promise.race([promise.then(() => false), new Promise(r => setTimeout(() => r(true), ms))]);
const ok = body => ({ _tag: 'HttpReply.Respond', response: { status: 200n, contentType: 'text/plain', body: new TextEncoder().encode(body) } });
const listen = async (handler, path, bounds = limits) => {
  const bound = deferred();
  const log = console.log;
  console.log = line => { console.log = log; bound.resolve(String(line).replace('listening http://', '')); };
  const fiber = Effect.runFork(path ? __ef_provider_LiveHttp.serve('127.0.0.1:0', path) : __ef_provider_LiveHttp.listen('127.0.0.1:0', bounds, handler));
  const [host, port] = (await bound.promise).split(':');
  return { fiber, host, port: Number(port), done: new Promise(resolve => fiber.addObserver(resolve)) };
};
const send = (server, path) => {
  const socket = net.connect(server.port, server.host);
  socket.write('GET ' + path + ' HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n');
  let data = '';
  const response = new Promise(resolve => {
    socket.on('data', chunk => { data += chunk; });
    socket.on('close', () => resolve(data));
    socket.on('error', () => {});
  });
  return { socket, response, status: response.then(text => Number(text.split(' ')[1])) };
};
const gated = (started, gate, after) => request => Effect.flatMap(
  Effect.acquireRelease(Effect.void, () => Effect.sync(() => started.resolve()).pipe(Effect.andThen(Effect.promise(() => gate.promise)))),
  () => after(request));
const reply = (contentType, body) => ({ _tag: 'HttpReply.Respond', response: { status: 200n, contentType, body } });
const large = new Uint8Array(32 << 20);
// A client that sends (pipelined) requests and stops reading once its first
// response has started arriving.
const stall = (server, ...paths) => {
  const socket = net.connect(server.port, server.host);
  const receiving = deferred();
  socket.on('error', () => {});
  socket.once('data', () => { socket.pause(); receiving.resolve(); });
  socket.write(paths.map(path => 'GET ' + path + ' HTTP/1.1\r\nHost: x\r\n\r\n').join(''));
  return { socket, receiving: receiving.promise };
};
const cleanedLarge = cleaned => () => Effect.flatMap(Effect.acquireRelease(Effect.void, () => Effect.sync(() => cleaned.resolve())), () => Effect.succeed(reply('application/octet-stream', large)));
const failures = [];
const check = (condition, message) => { if (!condition) failures.push(message); };

{ // publication waits for the request scope's cleanup
  const started = deferred(), gate = deferred();
  const server = await listen(gated(started, gate, () => Effect.succeed(ok('done'))));
  const client = send(server, '/');
  await started.promise;
  check(await pending(client.response), 'response published before cleanup completed');
  gate.resolve();
  const text = await client.response;
  check(text.startsWith('HTTP/1.1 200') && text.endsWith('done'), 'response after cleanup: ' + text);
  server.fiber.interruptUnsafe();
  await server.done;
}
{ // a cleanup failure after a successful handler is never published as success
  const server = await listen(() => Effect.flatMap(Effect.acquireRelease(Effect.void, () => Effect.die(new Error('cleanup failed'))), () => Effect.succeed(ok('never'))));
  const client = send(server, '/');
  check((await client.status) === 500, 'failed cleanup was not a generic 500');
  server.fiber.interruptUnsafe();
  await server.done;
}
{ // client disconnect cancels the handler and completes its cleanup
  const started = deferred(), cleaned = deferred();
  const server = await listen(() => Effect.flatMap(Effect.acquireRelease(Effect.sync(() => started.resolve()), () => Effect.sync(() => cleaned.resolve())), () => Effect.never));
  const client = send(server, '/');
  await started.promise;
  client.socket.destroy();
  check(!(await pending(cleaned.promise, 2000)), 'disconnect did not cancel the handler');
  server.fiber.interruptUnsafe();
  await server.done;
}
{ // shutdown cancels active work and answers 503 only after its cleanup
  const started = deferred(), gate = deferred(), active = deferred();
  const server = await listen(gated(started, gate, () => Effect.sync(() => active.resolve()).pipe(Effect.andThen(Effect.never))));
  const client = send(server, '/');
  await active.promise;
  server.fiber.interruptUnsafe();
  await started.promise;
  check(await pending(client.response), 'shutdown response published before cleanup');
  check(await pending(server.done), 'server completed before request cleanup');
  gate.resolve();
  const text = await client.response;
  check(text.startsWith('HTTP/1.1 503') && /connection: close/i.test(text), 'shutdown response: ' + text);
  const exit = await server.done;
  check(Exit.isFailure(exit) && Cause.hasInterruptsOnly(exit.cause), 'shutdown did not complete with interruption');
}
{ // an inadmissible header value is an empty 500 and the server keeps serving
  const server = await listen(request => Effect.succeed(request.path === '/bad' ? reply('text/λ', new Uint8Array(0)) : ok('fine')));
  const bad = await send(server, '/bad').response;
  check(bad.startsWith('HTTP/1.1 500') && !/content-type/i.test(bad), 'inadmissible header value: ' + bad);
  const good = await send(server, '/good').response;
  check(good.startsWith('HTTP/1.1 200') && good.endsWith('fine'), 'server after an inadmissible header value: ' + good);
  server.fiber.interruptUnsafe();
  await server.done;
}
if (backpressure) { // a queued response keeps its admission until the client received it or left
  const cleaned = deferred();
  const big = cleanedLarge(cleaned);
  const server = await listen(request => request.path === '/big' ? big() : Effect.succeed(ok('small')), null, { ...limits, maxActive: 1n });
  const stalled = stall(server, '/big');
  await cleaned.promise;
  await stalled.receiving;
  check((await send(server, '/second').status) === 503, 'a queued response released its admission');
  stalled.socket.destroy();
  let status;
  for (let attempt = 0; attempt < 100 && status !== 200; attempt++) {
    status = await send(server, '/third').status;
    if (status !== 200) await pending(new Promise(() => {}), 20);
  }
  check(status === 200, 'closing the stalled client did not restore admission: ' + status);
  server.fiber.interruptUnsafe();
  await server.done;
}
if (backpressure) { // shutdown joins the request scope, then aborts a response nobody reads
  const started = deferred(), cleaned = deferred();
  const server = await listen(request => request.path === '/big'
    ? Effect.succeed(reply('application/octet-stream', large))
    : Effect.flatMap(Effect.acquireRelease(Effect.sync(() => started.resolve()), () => Effect.sync(() => cleaned.resolve())), () => Effect.never), null, { ...limits, idleMillis: 200n });
  // The pipelined request's post-cleanup 503 queues behind the large
  // response, which the client stops reading.
  const stalled = stall(server, '/big', '/held');
  await started.promise;
  await stalled.receiving;
  server.fiber.interruptUnsafe();
  await cleaned.promise;
  check(!(await pending(server.done, 3000)), 'shutdown waited on a client that stopped reading');
  stalled.socket.destroy();
  await server.done;
}
{ // the raw path-to-text control keeps its decoded path and text responses
  const server = await listen(null, path => path === '/fail' ? Effect.fail({ _tag: 'Missing' }) : Effect.succeed('hi ' + path));
  const text = await send(server, '/a%20b').response;
  check(text.startsWith('HTTP/1.1 200') && /content-type: text\/plain; charset=utf-8/i.test(text) && text.endsWith('hi /a b'), 'raw path response: ' + text);
  const failed = await send(server, '/fail').response;
  check(failed.startsWith('HTTP/1.1 500') && failed.endsWith('Internal Server Error\n'), 'raw path failure: ' + failed);
  server.fiber.interruptUnsafe();
  await server.done;
}
if (failures.length > 0) throw new Error(failures.join('\n'));
console.log('ok');
`

func TestHTTPTransportJSPublishesAfterCleanupAndCancelsOnDisconnectAndShutdown(t *testing.T) {
	for host, output := range runJSOnHTTPHosts(t, `effect fn main()->void{let pending=Http.text("x").provide<Http>(LiveHttp); void}`, httpTransportJSHarness) {
		if strings.TrimSpace(output) != "ok" {
			t.Fatalf("%s: JS transport lifecycle: %s", host, output)
		}
	}
}

// runJSOnHTTPHosts runs one emitted module with assertions under Node and Bun.
// The module's directory links the repository's pinned node_modules, so both
// hosts resolve the same Effect.
func runJSOnHTTPHosts(t *testing.T, source, assertions string) map[string]string {
	t.Helper()
	r := CompileFor(source, "js")
	if !r.Checked {
		t.Fatalf("%+v", r.Diagnostics)
	}
	js, _, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	modules, err := filepath.Abs(filepath.Join("..", "..", "node_modules"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(modules, filepath.Join(dir, "node_modules")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "probe.mjs")
	if err := os.WriteFile(path, []byte(js+"\n"+assertions), 0644); err != nil {
		t.Fatal(err)
	}
	outputs := map[string]string{}
	for _, host := range []string{"node", "bun"} {
		binary, err := exec.LookPath(host)
		if err != nil {
			t.Fatalf("%s is required for the JS HTTP transport", host)
		}
		output, err := exec.Command(binary, path).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", host, err, output)
		}
		outputs[host] = string(output)
	}
	return outputs
}
