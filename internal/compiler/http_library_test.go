package compiler

import (
	"strings"
	"testing"
)

const (
	httpInterfaceOnlyLibrary = `effect fn greeting() -> bytes uses { Http } { run Http.text("hi") }
effect fn main() -> void { void }
`
	httpProvidingLibrary = `effect fn greeting() -> bytes uses { Http } { run Http.text("hi") }
effect fn provided() -> bytes raises { IoError } { run greeting().provide<Http>(LiveHttp) }
`
	// A layer is not a library export: only a provision retains its nodes.
	httpUnprovidedLayerLibrary = `layer Serving {
    Http = LiveHttp
}
effect fn greeting() -> bytes uses { Http } { run Http.text("hi") }
`
	httpProvidedLayerLibrary = httpUnprovidedLayerLibrary + `effect fn served() -> bytes { run greeting().provide(Serving) }
`
	httpLocalNameLibrary = `effect fn main() -> string {
    let LiveHttp = "local"
    LiveHttp
}
`
)

// The JavaScript library surface roots every module function and provider, so
// retaining LiveHttp is plan reachability from those roots. Library output
// carries the transport exactly where checked code selects LiveHttp, and a
// library that only requires the Http interface neither exports nor embeds it.
func TestJSLibraryExportsLiveHttpOnlyWhereCheckedCodeSelectsIt(t *testing.T) {
	for _, test := range []struct {
		name, source string
		exported     bool
	}{
		{"interface only", httpInterfaceOnlyLibrary, false},
		{"local binding named LiveHttp", httpLocalNameLibrary, false},
		{"provided by a module function", httpProvidingLibrary, true},
		{"selected by a provided layer", httpProvidedLayerLibrary, true},
		{"selected by a layer nothing provides", httpUnprovidedLayerLibrary, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := CompileFor(test.source, "js")
			if !r.Checked {
				t.Fatal(r.Diagnostics)
			}
			library, _, err := r.Emit(false)
			if err != nil {
				t.Fatal(err)
			}
			exported := strings.Contains(library, "as LiveHttp}") || strings.Contains(library, "as LiveHttp ")
			embedded := strings.Contains(library, "__ef_provider_LiveHttp = {")
			if exported != test.exported || embedded != test.exported {
				t.Fatalf("LiveHttp exported %v, transport embedded %v, want both %v", exported, embedded, test.exported)
			}
		})
	}
}

// Retained service tags never imply a provider: Http requirements are
// exported as tags and types for every library that admits Http.
func TestJSLibraryInterfaceOnlyHttpKeepsServiceButNotTransport(t *testing.T) {
	r := CompileFor(httpInterfaceOnlyLibrary, "js")
	library, declarations, err := r.Emit(false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(library, "Context.Service(\"effra/prototype/Http\")") || !strings.Contains(declarations, "HttpRequest") {
		t.Fatalf("interface-only library lost the Http service or data:\n%s", library)
	}
	if strings.Contains(library, "node:http") {
		t.Fatal("interface-only library embeds the transport")
	}
}
