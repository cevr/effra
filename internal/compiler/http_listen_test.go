package compiler

import (
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// httpListenDNSStub replaces Node's promise lookup, the host DNS boundary the
// JavaScript HTTP adapter imports, before the probe loads. Each name answers
// once; a second lookup of the same name fails, and an unknown name never
// resolves. net.Server.listen resolves names through the callback lookup,
// which is not stubbed, so a listener handed a name instead of its numeric
// answer fails to bind.
const httpListenDNSStub = `import { createRequire, syncBuiltinESMExports } from 'node:module';
const require = createRequire(import.meta.url);
const promises = require('node:dns/promises');
const answers = {
  'dual.example': [{ address: '::1', family: 6 }, { address: '127.0.0.1', family: 4 }],
  'six.example': [{ address: '::1', family: 6 }],
  'many.example': [{ address: '127.0.0.2', family: 4 }, { address: '127.0.0.1', family: 4 }],
  'mapped.example': [{ address: '::1', family: 6 }, { address: '::ffff:127.0.0.3', family: 6 }],
  'mapped-first.example': [{ address: '::ffff:127.0.0.4', family: 6 }, { address: '127.0.0.5', family: 4 }],
  'zoned.example': [{ address: 'fe80::1%lo', family: 6 }, { address: '::ffff:7f00:7%0', family: 6 }],
  'zone-name.example': [{ address: '::1', family: 6 }, { address: '::ffff:7f00:8%lo_0', family: 6 }],
  'bad-zones.example': [{ address: '127.0.0.1%0', family: 4 }, { address: '::1%', family: 6 }],
  'empty.example': [],
};
const calls = globalThis.__dnsCalls = [];
promises.lookup = async (host, options) => {
  calls.push(host + (options?.all === true ? ' all' : ''));
  if (calls.filter(call => call.split(' ')[0] === host).length > 1 || !(host in answers)) {
    throw Object.assign(new Error('getaddrinfo ENOTFOUND ' + host), { code: 'ENOTFOUND' });
  }
  return answers[host];
};
syncBuiltinESMExports();
`

// httpListenProbe first selects every address's listen host without binding,
// through the adapter's production lookup, then serves the addresses the
// harness found bindable (EFFRA_BINDABLE) and reports the bound address.
const httpListenProbe = `const { isIP } = await import('node:net');
const { lookup } = await import('node:dns/promises');
const failure = exit => exit.cause.reasons.map(reason => reason.error?.message ?? reason._tag).join('; ');
// Each stage forgets the lookups it made, so the serve stage starts with
// every name unlooked-up and the stub still fails a second lookup within it.
for (const address of globalThis.__selectAddresses) {
  const before = globalThis.__dnsCalls.length;
  const exit = await Effect.runPromiseExit(__ef_http_listen_target(address, isIP, lookup));
  const calls = globalThis.__dnsCalls.splice(before);
  console.log('select ' + address + ' -> ' + (Exit.isSuccess(exit) ? exit.value.host : failure(exit)) + ' (lookups ' + JSON.stringify(calls) + ')');
}
const limits = { readHeaderMillis: 1000, readBodyMillis: 1000, idleMillis: 1000, maxActive: 1, maxBodyBytes: 0 };
for (const address of globalThis.__serveAddresses) {
  const before = globalThis.__dnsCalls.length;
  const logged = [];
  const log = console.log;
  console.log = line => { logged.push(String(line)); };
  const fiber = Effect.runFork(__ef_http_serve(address, limits, () => {}));
  let exit;
  fiber.addObserver(done => { exit = done; });
  for (let wait = 0; logged.length === 0 && exit === undefined && wait < 500; wait++) await new Promise(resolve => setTimeout(resolve, 10));
  if (exit === undefined) await Effect.runPromise(Fiber.interrupt(fiber));
  console.log = log;
  const outcome = logged.length > 0 ? logged[0].replace(/:[0-9]+$/, ':PORT') : failure(exit);
  console.log('serve ' + address + ' -> ' + outcome + ' (lookups ' + JSON.stringify(globalThis.__dnsCalls.slice(before)) + ')');
}
`

// httpListenCase is one listen address: the host the adapter selects for it,
// and, when it binds, the address the listener reports.
type httpListenCase struct{ address, selected, lookups, bound string }

var httpListenCases = []httpListenCase{
	{"dual.example:0", "127.0.0.1", `["dual.example all"]`, "127.0.0.1"},
	{"six.example:0", "::1", `["six.example all"]`, "[::1]"},
	{"many.example:0", "127.0.0.2", `["many.example all"]`, "127.0.0.2"},
	{"mapped.example:0", "127.0.0.3", `["mapped.example all"]`, "127.0.0.3"},
	{"mapped-first.example:0", "127.0.0.4", `["mapped-first.example all"]`, "127.0.0.4"},
	// A zoned mapped answer is IPv4 without its zone; the zoned link-local
	// answer before it is genuine IPv6 and not preferred.
	{"zoned.example:0", "127.0.0.7", `["zoned.example all"]`, "127.0.0.7"},
	// A zone is any non-empty text, as Go's netip.ParseAddr reads it; hosts
	// that cannot parse it as part of an address must not see it whole.
	{"zone-name.example:0", "127.0.0.8", `["zone-name.example all"]`, "127.0.0.8"},
	// An empty zone or a zone on dotted IPv4 is not a numeric address, so
	// such answers are discarded and such literals are looked up as names.
	{"bad-zones.example:0", `HTTP listen on "bad-zones.example:0": host lookup failed`, `["bad-zones.example all"]`, ""},
	{"[::ffff:127.0.0.6]:0", "127.0.0.6", `[]`, "127.0.0.6"},
	{"[::ffff:7f00:2%0]:0", "127.0.0.2", `[]`, "127.0.0.2"},
	{"[::ffff:7f00:2%lo_0]:0", "127.0.0.2", `[]`, "127.0.0.2"},
	{"[::1%]:0", `HTTP listen on "[::1%]:0": host lookup failed`, `["::1% all"]`, ""},
	{"127.0.0.1%0:0", `HTTP listen on "127.0.0.1%0:0": host lookup failed`, `["127.0.0.1%0 all"]`, ""},
	// A genuine IPv6 literal keeps its zone. Selection only: no interface on
	// a test host is guaranteed to own fe80::1.
	{"[fe80::1%lo]:0", "fe80::1%lo", `[]`, ""},
	{"missing.example:0", `HTTP listen on "missing.example:0": host lookup failed`, `["missing.example all"]`, ""},
	{"empty.example:0", `HTTP listen on "empty.example:0": host lookup failed`, `["empty.example all"]`, ""},
	{"127.0.0.1:0", "127.0.0.1", `[]`, "127.0.0.1"},
}

// The JavaScript HTTP adapter looks a listen host name up exactly once and
// binds the selected numeric answer: the first IPv4 answer by Go's IP.To4,
// which includes IPv4-mapped IPv6 addresses (zone dropped) bound in their
// IPv4 form, else the first answer, as Go's net.Listen does. Selection is
// asserted on every host. Binding is asserted for each address this host can
// bind, probed once; the others are skipped and logged.
func TestHTTPListenResolvesAHostNameOnceAndBindsTheSelectedAnswer(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node is required for JavaScript emission tests")
	}
	bindable := map[string]bool{}
	var serve, skipped []string
	var want strings.Builder
	selectAddresses := []string{}
	for _, test := range httpListenCases {
		selectAddresses = append(selectAddresses, test.address)
		want.WriteString("select " + test.address + " -> " + test.selected + " (lookups " + test.lookups + ")\n")
	}
	for _, test := range httpListenCases {
		if test.bound == "" {
			continue
		}
		host := strings.Trim(test.bound, "[]")
		ok, probed := bindable[host]
		if !probed {
			listener, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
			if ok = err == nil; ok {
				_ = listener.Close()
			}
			bindable[host] = ok
		}
		if !ok {
			skipped = append(skipped, test.address+" ("+host+")")
			continue
		}
		serve = append(serve, test.address)
		want.WriteString("serve " + test.address + " -> listening http://" + test.bound + ":PORT (lookups " + test.lookups + ")\n")
	}
	if len(skipped) > 0 {
		t.Logf("binding skipped, address not bindable on this host: %s", strings.Join(skipped, ", "))
	}
	source := `effect fn route(request: HttpRequest) -> HttpReply uses { Http } {
    let body = run Http.text("")
    HttpReply.Respond { response: HttpResponse { status: 200, contentType: "", body: body } }
}
effect fn main() -> void raises { IoError } {
    let limits = HttpLimits { maxBodyBytes: 0, readHeaderMillis: 5000, readBodyMillis: 5000, idleMillis: 5000, maxActive: 1 }
    run Http.listen("127.0.0.1:0", limits, route).provide<Http>(LiveHttp)
}
`
	r := CompileFor(source, "js")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	library, _, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	quote := func(items []string) string {
		quoted := []string{}
		for _, item := range items {
			quoted = append(quoted, "'"+item+"'")
		}
		return "[" + strings.Join(quoted, ", ") + "]"
	}
	setup := "globalThis.__selectAddresses = " + quote(selectAddresses) + ";\nglobalThis.__serveAddresses = " + quote(serve) + ";\n"
	dir := writeJSModule(t, map[string]string{"dns-stub.mjs": httpListenDNSStub, "probe.mjs": withProbeImports(library) + "\n" + setup + httpListenProbe})
	stub, probe := filepath.Join(dir, "dns-stub.mjs"), filepath.Join(dir, "probe.mjs")
	output, err := exec.Command(node, "--preserve-symlinks", "--preserve-symlinks-main", "--import", "file://"+stub, probe).CombinedOutput()
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, output)
	}
	if string(output) != want.String() {
		t.Fatalf("got:\n%s\nwant:\n%s", output, want.String())
	}
}
