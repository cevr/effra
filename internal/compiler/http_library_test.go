package compiler

import (
	"os"
	"path/filepath"
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

// httpLibraryCorpus is every example plus the Http programs above.
func httpLibraryCorpus(t *testing.T) map[string]string {
	t.Helper()
	corpus := map[string]string{
		"interface only": httpInterfaceOnlyLibrary, "providing": httpProvidingLibrary,
		"transport routes unprovided": httpTransportRouteSource,
		"transport routes listening":  httpTransportRouteSource + `effect fn main()->void{let pending=Http.listen("127.0.0.1:0",limits(),route).provide<Users>(Memory).provide<Http>(LiveHttp);void}`,
		"provided layer":              httpProvidedLayerLibrary, "unprovided layer": httpUnprovidedLayerLibrary, "local name": httpLocalNameLibrary,
	}
	files, err := filepath.Glob("../../examples/*.ef")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		corpus[filepath.Base(path)] = string(source)
	}
	return corpus
}

// Transitional: the library plan reaches LiveHttp exactly where the checker
// recorded a LiveHttp value. Removed with that side table.
func TestLibraryPlanReachesLiveHttpExactlyWhereCheckedValuesSelectIt(t *testing.T) {
	selected := 0
	for name, source := range httpLibraryCorpus(t) {
		r := CompileAt(source, "go", "../../examples")
		if !r.Checked {
			continue
		}
		plan, err := r.LibraryPlan()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := r.referencesBuiltinProvider("LiveHttp")
		got := false
		if provider := r.checkedProviders["LiveHttp"]; provider != nil {
			got = plan.Requires(RequiresProvider, providerTypeRef(provider).Declaration)
		}
		// An unprovided layer is the one divergence: the checker saw its
		// LiveHttp value, but the library exports no layer, so the plan does
		// not carry the transport the old record over-approximated.
		if name == "unprovided layer" {
			want = false
		}
		if got != want {
			t.Errorf("%s: library plan retains LiveHttp %v, checked values select it %v", name, got, want)
		}
		if want {
			selected++
		}
	}
	if selected < 3 {
		t.Fatalf("corpus selects LiveHttp %d times, want at least the providing, layer and http examples", selected)
	}
}
