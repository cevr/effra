package compiler

import (
	"strings"
	"testing"
)

// The JavaScript HTTP adapter looks a listen host name up exactly once and
// binds the selected numeric answer, the first IPv4 one or else the first,
// as Go's net.Listen does. The resolver is stubbed at the adapter's DNS
// boundary: every lookup after the first fails, so a second lookup would
// surface as a failure, and the listener for a name that does not exist can
// only bind because it received the numeric answer.
func TestHTTPListenResolvesAHostNameOnceAndBindsTheSelectedAnswer(t *testing.T) {
	source := `effect fn route(request: HttpRequest) -> HttpReply uses { Http } {
    let body = run Http.text("")
    HttpReply.Respond { response: HttpResponse { status: 200, contentType: "", body: body } }
}
effect fn main() -> void raises { IoError } {
    let limits = HttpLimits { maxBodyBytes: 0, readHeaderMillis: 5000, readBodyMillis: 5000, idleMillis: 5000, maxActive: 1 }
    run Http.listen("127.0.0.1:0", limits, route).provide<Http>(LiveHttp)
}
`
	probe := `const { isIP } = await import('node:net');
const stub = answers => {
  const calls = [];
  return { calls, resolver: { isIP, lookup: async (host, options) => {
    calls.push({ host, all: options?.all === true });
    if (calls.length > 1) throw Object.assign(new Error('getaddrinfo ENOTFOUND ' + host), { code: 'ENOTFOUND' });
    if (answers instanceof Error) throw answers;
    return answers;
  } } };
};
const target = async (address, answers) => {
  const { calls, resolver } = stub(answers);
  const exit = await Effect.runPromiseExit(__ef_http_listen_target(address, resolver));
  const outcome = Exit.isSuccess(exit) ? exit.value.host : exit.cause.reasons.map(reason => reason.error?.message ?? reason._tag).join('; ');
  console.log(address + ' -> ' + outcome + ' (lookups ' + JSON.stringify(calls) + ')');
};
await target('dual.example:0', [{ address: '::1', family: 6 }, { address: '127.0.0.1', family: 4 }]);
await target('six.example:0', [{ address: '::1', family: 6 }]);
await target('many.example:0', [{ address: '127.0.0.2', family: 4 }, { address: '127.0.0.1', family: 4 }]);
await target('missing.example:0', Object.assign(new Error('getaddrinfo ENOTFOUND missing.example'), { code: 'ENOTFOUND' }));
await target('empty.example:0', []);
await target('127.0.0.1:0', []);
const { calls, resolver } = stub([{ address: '127.0.0.1', family: 4 }]);
const logged = [];
const log = console.log;
console.log = line => { logged.push(String(line)); };
const fiber = Effect.runFork(__ef_http_serve('changing.example:0', { readHeaderMillis: 1000, readBodyMillis: 1000, idleMillis: 1000, maxActive: 1, maxBodyBytes: 0 }, () => {}, resolver));
for (let wait = 0; logged.length === 0 && wait < 500; wait++) await new Promise(resolve => setTimeout(resolve, 10));
await Effect.runPromise(Fiber.interrupt(fiber));
console.log = log;
console.log('serve changing.example:0 -> ' + logged.map(line => line.replace(/:[0-9]+$/, ':PORT')).join('; ') + ' (lookups ' + calls.length + ')');
`
	want := strings.Join([]string{
		`dual.example:0 -> 127.0.0.1 (lookups [{"host":"dual.example","all":true}])`,
		`six.example:0 -> ::1 (lookups [{"host":"six.example","all":true}])`,
		`many.example:0 -> 127.0.0.2 (lookups [{"host":"many.example","all":true}])`,
		`missing.example:0 -> HTTP listen on "missing.example:0": host lookup failed (lookups [{"host":"missing.example","all":true}])`,
		`empty.example:0 -> HTTP listen on "empty.example:0": host lookup failed (lookups [{"host":"empty.example","all":true}])`,
		`127.0.0.1:0 -> 127.0.0.1 (lookups [])`,
		`serve changing.example:0 -> listening http://127.0.0.1:PORT (lookups 1)`,
	}, "\n") + "\n"
	if got := runJS(t, source, probe); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
