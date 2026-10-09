package compiler

import (
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

const httpListenProbe = `const limits = { readHeaderMillis: 1000, readBodyMillis: 1000, idleMillis: 1000, maxActive: 1, maxBodyBytes: 0 };
const serve = async address => {
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
  const outcome = logged.length > 0 ? logged[0].replace(/:[0-9]+$/, ':PORT') : exit.cause.reasons.map(reason => reason.error?.message ?? reason._tag).join('; ');
  console.log(address + ' -> ' + outcome + ' (lookups ' + JSON.stringify(globalThis.__dnsCalls.slice(before)) + ')');
};
for (const address of ['dual.example:0', 'six.example:0', 'many.example:0', 'mapped.example:0', 'mapped-first.example:0', 'missing.example:0', 'empty.example:0', '[::ffff:127.0.0.6]:0', '127.0.0.1:0']) await serve(address);
`

// The JavaScript HTTP adapter looks a listen host name up exactly once and
// binds the selected numeric answer: the first IPv4 answer by Go's IP.To4,
// which includes IPv4-mapped IPv6 addresses bound in their IPv4 form, else
// the first answer, as Go's net.Listen does. The probe runs the production
// listener under Node with the DNS boundary stubbed, so a second lookup, a
// listener given the name, a family-only selection or a lookup that never
// answers each change the transcript.
func TestHTTPListenResolvesAHostNameOnceAndBindsTheSelectedAnswer(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("Node is required for JavaScript emission tests")
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
	dir := writeJSModule(t, map[string]string{"dns-stub.mjs": httpListenDNSStub, "probe.mjs": withProbeImports(library) + "\n" + httpListenProbe})
	stub, probe := filepath.Join(dir, "dns-stub.mjs"), filepath.Join(dir, "probe.mjs")
	output, err := exec.Command(node, "--preserve-symlinks", "--preserve-symlinks-main", "--import", "file://"+stub, probe).CombinedOutput()
	if err != nil {
		t.Fatalf("probe: %v\n%s", err, output)
	}
	want := strings.Join([]string{
		`dual.example:0 -> listening http://127.0.0.1:PORT (lookups ["dual.example all"])`,
		`six.example:0 -> listening http://[::1]:PORT (lookups ["six.example all"])`,
		`many.example:0 -> listening http://127.0.0.2:PORT (lookups ["many.example all"])`,
		`mapped.example:0 -> listening http://127.0.0.3:PORT (lookups ["mapped.example all"])`,
		`mapped-first.example:0 -> listening http://127.0.0.4:PORT (lookups ["mapped-first.example all"])`,
		`missing.example:0 -> HTTP listen on "missing.example:0": host lookup failed (lookups ["missing.example all"])`,
		`empty.example:0 -> HTTP listen on "empty.example:0": host lookup failed (lookups ["empty.example all"])`,
		`[::ffff:127.0.0.6]:0 -> listening http://127.0.0.6:PORT (lookups [])`,
		`127.0.0.1:0 -> listening http://127.0.0.1:PORT (lookups [])`,
	}, "\n") + "\n"
	if string(output) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", output, want)
	}
}
