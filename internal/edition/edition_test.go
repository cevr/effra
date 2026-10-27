package edition

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// workspace returns a fresh directory with symlinks resolved. No ancestor of
// it may hold a go.mod or an edition manifest, or every expectation below
// would depend on the host.
func workspace(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for dir := filepath.Dir(root); ; dir = filepath.Dir(dir) {
		for _, name := range []string{"go.mod", ManifestName} {
			if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
				t.Fatalf("the test directory's ancestor %s holds %s; run the tests with TMPDIR outside any module", dir, name)
			}
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	return root
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// resolveNow runs Resolve with a deadline, so a manifest that blocks a read
// fails the test instead of hanging it.
func resolveNow(t *testing.T, dir string) Resolution {
	t.Helper()
	done := make(chan Resolution, 1)
	go func() { done <- Resolve(dir) }()
	select {
	case r := <-done:
		return r
	case <-time.After(10 * time.Second):
		t.Fatalf("Resolve(%s) did not return within 10s", dir)
		return Resolution{}
	}
}

func wantEdition(t *testing.T, dir string, edition Edition, source, manifest string) {
	t.Helper()
	r := resolveNow(t, dir)
	if r.Diagnostic != nil || r.Edition != edition || r.Source != source || r.Manifest != manifest {
		t.Fatalf("Resolve(%s) = %s, want edition %q source %q manifest %q", dir, r.JSON(), edition, source, manifest)
	}
}

func wantRefusal(t *testing.T, dir, manifest string, fragments ...string) Resolution {
	t.Helper()
	r := resolveNow(t, dir)
	if r.Diagnostic == nil || r.Diagnostic.Code != CodeUnsupported || r.Edition != "" || r.Source != SourceManifest || r.Manifest != manifest {
		t.Fatalf("Resolve(%s) = %s, want EF144 for %s", dir, r.JSON(), manifest)
	}
	for _, fragment := range append([]string{manifest, `"legacy"`, `"2026"`}, fragments...) {
		if !strings.Contains(r.Diagnostic.Message, fragment) {
			t.Fatalf("EF144 message %q does not name %q", r.Diagnostic.Message, fragment)
		}
	}
	want := `{"edition":null,"source":"manifest","manifest":` + quoteJSON(manifest) + `,"diagnostic":{"code":"EF144","message":`
	if !strings.HasPrefix(string(r.JSON()), want) {
		t.Fatalf("EF144 projection %s, want prefix %s", r.JSON(), want)
	}
	return r
}

func quoteJSON(text string) string {
	data, err := json.Marshal(text)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func TestResolveNearestManifestWins(t *testing.T) {
	root := workspace(t)
	write(t, filepath.Join(root, "go.mod"), "module m\n")
	write(t, filepath.Join(root, ManifestName), `{"edition":"2026"}`)
	write(t, filepath.Join(root, "a", "b", ManifestName), `{"edition":"legacy"}`)
	mkdir(t, filepath.Join(root, "a", "b", "c"))
	top := filepath.Join(root, ManifestName)
	nested := filepath.Join(root, "a", "b", ManifestName)
	wantEdition(t, root, E2026, SourceManifest, top)
	wantEdition(t, filepath.Join(root, "a"), E2026, SourceManifest, top)
	wantEdition(t, filepath.Join(root, "a", "b"), Legacy, SourceManifest, nested)
	wantEdition(t, filepath.Join(root, "a", "b", "c"), Legacy, SourceManifest, nested)
	// The nearest manifest decides even when it is refused: an ancestor's
	// valid manifest is never a fallback.
	write(t, filepath.Join(root, "x", ManifestName), `{"edition":"2099"}`)
	mkdir(t, filepath.Join(root, "x", "y"))
	wantRefusal(t, filepath.Join(root, "x", "y"), filepath.Join(root, "x", ManifestName), `"2099"`)
	// A relative directory resolves against the working directory, and the
	// manifest path is absolute and clean.
	t.Chdir(filepath.Join(root, "a"))
	wantEdition(t, filepath.Join(".", "b", "..", "b", "c"), Legacy, SourceManifest, nested)
}

func TestResolveStopsAtGoModuleRoot(t *testing.T) {
	root := workspace(t)
	write(t, filepath.Join(root, ManifestName), `{"edition":"2026"}`)
	// A manifest above the nearest go.mod directory is ignored.
	write(t, filepath.Join(root, "mod", "go.mod"), "module mod\n")
	mkdir(t, filepath.Join(root, "mod", "pkg"))
	wantEdition(t, filepath.Join(root, "mod"), Legacy, SourceDefault, "")
	wantEdition(t, filepath.Join(root, "mod", "pkg"), Legacy, SourceDefault, "")
	// A manifest beside go.mod counts.
	write(t, filepath.Join(root, "beside", "go.mod"), "module beside\n")
	write(t, filepath.Join(root, "beside", ManifestName), `{"edition":"legacy"}`)
	mkdir(t, filepath.Join(root, "beside", "pkg"))
	wantEdition(t, filepath.Join(root, "beside", "pkg"), Legacy, SourceManifest, filepath.Join(root, "beside", ManifestName))
	// A directory named go.mod is no module root, as in Go.
	mkdir(t, filepath.Join(root, "fake", "go.mod"))
	mkdir(t, filepath.Join(root, "fake", "pkg"))
	wantEdition(t, filepath.Join(root, "fake", "pkg"), E2026, SourceManifest, filepath.Join(root, ManifestName))
	// With no go.mod, the walk continues to the filesystem root: the
	// manifest at the workspace top governs a deep directory.
	deep := filepath.Join(root, "a", "b", "c", "d", "e", "f")
	mkdir(t, deep)
	wantEdition(t, deep, E2026, SourceManifest, filepath.Join(root, ManifestName))
}

func TestResolveDefaultsToLegacyWithoutManifest(t *testing.T) {
	root := workspace(t)
	for _, dir := range []string{root, filepath.Join(root, "missing", "dir")} {
		r := resolveNow(t, dir)
		if got, want := string(r.JSON()), `{"edition":"legacy","source":"default","manifest":""}`; got != want {
			t.Fatalf("Resolve(%s) = %s, want %s", dir, got, want)
		}
	}
	write(t, filepath.Join(root, "mod", "go.mod"), "module mod\n")
	wantEdition(t, filepath.Join(root, "mod"), Legacy, SourceDefault, "")
}

func TestResolveRejectsUnsupportedEdition(t *testing.T) {
	root := workspace(t)
	for index, value := range []string{`""`, `"2099"`, `"Legacy"`, `" 2026"`, `"sketch"`, `"2026 "`, `"LEGACY"`, `2026`, `null`, `true`, `["2026"]`, `{"id":"2026"}`} {
		dir := filepath.Join(root, fmt.Sprint(index))
		write(t, filepath.Join(dir, ManifestName), `{"edition":`+value+`}`)
		wantRefusal(t, dir, filepath.Join(dir, ManifestName), value)
	}
	// The supported ids, with insignificant whitespace, are admitted.
	for index, id := range Supported {
		dir := filepath.Join(root, "ok", fmt.Sprint(index))
		write(t, filepath.Join(dir, ManifestName), "\n{ \"edition\" : \""+string(id)+"\" }\n")
		wantEdition(t, dir, id, SourceManifest, filepath.Join(dir, ManifestName))
	}
	// The surfaces' shared fixture: a valid earlier-language program under
	// {"edition": "2099"}.
	fixture, err := filepath.Abs(filepath.Join("testdata", "unsupported"))
	if err != nil {
		t.Fatal(err)
	}
	wantRefusal(t, fixture, filepath.Join(fixture, ManifestName), `"2099"`)
}

func TestResolveRejectsMalformedManifest(t *testing.T) {
	root := workspace(t)
	oversize := `{"edition":"2026"}` + strings.Repeat(" ", MaxManifestBytes)
	cases := []struct {
		name, content, reason string
	}{
		{"invalid JSON", `{"edition":`, "not valid JSON"},
		{"empty file", ``, "not valid JSON"},
		{"unknown field", `{"edition":"2026","extra":1}`, `unknown field "extra"`},
		{"field case", `{"Edition":"2026"}`, `unknown field "Edition"`},
		{"repeated field", `{"edition":"legacy","edition":"2026"}`, "repeats"},
		{"trailing object", `{"edition":"2026"} {}`, "data after"},
		{"trailing text", `{"edition":"2026"}x`, "data after"},
		{"missing edition", `{}`, `no "edition"`},
		{"array", `["2026"]`, "not a JSON object"},
		{"string", `"2026"`, "not a JSON object"},
		{"over 64 KiB", oversize, "exceeds 65536 bytes"},
	}
	for index, c := range cases {
		dir := filepath.Join(root, fmt.Sprint(index))
		write(t, filepath.Join(dir, ManifestName), c.content)
		t.Run(c.name, func(t *testing.T) { wantRefusal(t, dir, filepath.Join(dir, ManifestName), c.reason) })
	}
	t.Run("directory", func(t *testing.T) {
		dir := filepath.Join(root, "directory")
		mkdir(t, filepath.Join(dir, ManifestName))
		wantRefusal(t, dir, filepath.Join(dir, ManifestName), "not a regular file")
	})
	t.Run("dangling link", func(t *testing.T) {
		dir := filepath.Join(root, "dangling")
		mkdir(t, dir)
		if err := os.Symlink(filepath.Join(dir, "absent.json"), filepath.Join(dir, ManifestName)); err != nil {
			t.Skip("symlinks unavailable:", err)
		}
		wantRefusal(t, dir, filepath.Join(dir, ManifestName), "missing file")
	})
	t.Run("FIFO", func(t *testing.T) {
		dir := filepath.Join(root, "fifo")
		mkdir(t, dir)
		if err := makeFIFO(filepath.Join(dir, ManifestName)); err != nil {
			t.Skip("FIFOs unavailable:", err)
		}
		// resolveNow fails the test if the read blocks on the FIFO.
		wantRefusal(t, dir, filepath.Join(dir, ManifestName), "not a regular file")
	})
	t.Run("fixture", func(t *testing.T) {
		fixture, err := filepath.Abs(filepath.Join("testdata", "malformed"))
		if err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, fixture, filepath.Join(fixture, ManifestName), "repeats")
	})
}

// A generated directory's contents.
const (
	manifestNone = iota
	manifestLegacy
	manifestE2026
	manifestUnknownID
	manifestMalformed
	manifestUnknownField
	manifestMissingEdition
	manifestDirectory
	manifestKinds
)

type generatedDir struct {
	rel      string
	goMod    bool
	manifest int
}

// TestResolveProperty compares Resolve on seeded random trees with an
// oracle that scans the list of a directory's ancestors, nearest first.
func TestResolveProperty(t *testing.T) {
	base := workspace(t)
	const trees = 500
	checked, documentsChecked := 0, 0
	for seed := uint64(1); seed <= trees; seed++ {
		random := rand.New(rand.NewPCG(seed, 0x194))
		root := filepath.Join(base, fmt.Sprint(seed))
		var dirs []generatedDir
		var grow func(rel string, depth int)
		grow = func(rel string, depth int) {
			dir := generatedDir{rel: rel, goMod: random.IntN(4) == 0}
			if random.IntN(2) == 0 {
				dir.manifest = 1 + random.IntN(manifestKinds-1)
			}
			dirs = append(dirs, dir)
			if depth == 6 {
				return
			}
			for child := range random.IntN(3) + boolInt(depth == 1) {
				grow(filepath.Join(rel, fmt.Sprintf("d%d", child)), depth+1)
			}
		}
		grow(".", 1)
		byRel := map[string]generatedDir{}
		for _, dir := range dirs {
			byRel[dir.rel] = dir
			path := filepath.Join(root, dir.rel)
			mkdir(t, path)
			if dir.goMod {
				write(t, filepath.Join(path, "go.mod"), "module generated\n")
			}
			manifest := filepath.Join(path, ManifestName)
			switch dir.manifest {
			case manifestLegacy:
				write(t, manifest, `{"edition":"legacy"}`)
			case manifestE2026:
				write(t, manifest, `{"edition":"2026"}`)
			case manifestUnknownID:
				write(t, manifest, `{"edition":"`+unknownID(random)+`"}`)
			case manifestMalformed:
				write(t, manifest, []string{`{"edition":`, `{"edition":"2026"}}`, `[]`, `not json`}[random.IntN(4)])
			case manifestUnknownField:
				write(t, manifest, `{"edition":"2026","name":"x"}`)
			case manifestMissingEdition:
				write(t, manifest, `{}`)
			case manifestDirectory:
				mkdir(t, manifest)
			}
		}
		for _, dir := range dirs {
			got := Resolve(filepath.Join(root, dir.rel))
			want := oracle(root, dir.rel, byRel)
			if got.Edition != want.Edition || got.Source != want.Source || got.Manifest != want.Manifest || (got.Diagnostic == nil) != (want.Diagnostic == nil) || got.Diagnostic != nil && got.Diagnostic.Code != CodeUnsupported {
				t.Fatalf("seed %d: Resolve(%s) = %s, oracle %s", seed, dir.rel, got.JSON(), want.JSON())
			}
			checked++
		}
		// Unsaved documents spelled through links: a directory link with a
		// missing suffix, and a dangling final link with a relative or an
		// absolute target. Each resolves where its link leads.
		for index := range 2 {
			target := dirs[random.IntN(len(dirs))].rel
			link := filepath.Join(root, fmt.Sprintf("L%d", index))
			if err := os.Symlink(filepath.Join(root, target), link); err != nil {
				t.Fatal(err)
			}
			spelled := link
			for missing := range random.IntN(3) {
				spelled = filepath.Join(spelled, fmt.Sprintf("m%d", missing))
			}
			documents := []string{filepath.Join(spelled, "unsaved.ef")}
			dangling := filepath.Join(root, fmt.Sprintf("D%d.ef", index))
			linkTarget := filepath.Join(root, target, "absent.ef")
			if random.IntN(2) == 0 {
				linkTarget = filepath.Join(target, "absent.ef")
			}
			if err := os.Symlink(linkTarget, dangling); err != nil {
				t.Fatal(err)
			}
			documents = append(documents, dangling)
			for _, document := range documents {
				got := ResolveDocument(document)
				want := oracle(root, target, byRel)
				if got.Edition != want.Edition || got.Source != want.Source || got.Manifest != want.Manifest || (got.Diagnostic == nil) != (want.Diagnostic == nil) || got.Diagnostic != nil && got.Diagnostic.Code != CodeUnsupported {
					t.Fatalf("seed %d: ResolveDocument(%s) = %s, oracle for %s %s", seed, document, got.JSON(), target, want.JSON())
				}
				documentsChecked++
			}
		}
	}
	t.Logf("%d trees, %d directories, %d linked documents", trees, checked, documentsChecked)
}

// oracle scans rel's ancestors up to the tree root, nearest first, as a
// plain list. Above the root nothing is present (workspace checks it).
func oracle(root, rel string, dirs map[string]generatedDir) Resolution {
	var ancestors []string
	for current := rel; ; {
		ancestors = append(ancestors, current)
		if current == "." {
			break
		}
		current = filepath.Dir(current)
	}
	for _, ancestor := range ancestors {
		dir := dirs[ancestor]
		manifest := filepath.Join(root, ancestor, ManifestName)
		switch dir.manifest {
		case manifestNone:
		case manifestLegacy:
			return Resolution{Edition: Legacy, Source: SourceManifest, Manifest: manifest}
		case manifestE2026:
			return Resolution{Edition: E2026, Source: SourceManifest, Manifest: manifest}
		default:
			return Resolution{Source: SourceManifest, Manifest: manifest, Diagnostic: &Diagnostic{Code: CodeUnsupported}}
		}
		if dir.goMod {
			break
		}
	}
	return Resolution{Edition: Legacy, Source: SourceDefault}
}

func unknownID(random *rand.Rand) string {
	for {
		letters := []byte("abcLEGACY2026 _-")
		id := make([]byte, random.IntN(8))
		for i := range id {
			id[i] = letters[random.IntN(len(letters))]
		}
		if !slices.Contains(Supported, Edition(id)) {
			return string(id)
		}
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// TestEditionDiagnosticsAreDocumented keeps the declared codes and the
// edition table of docs/tooling.md in step, both ways.
func TestEditionDiagnosticsAreDocumented(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "tooling.md"))
	if err != nil {
		t.Fatal(err)
	}
	const heading = "### Edition diagnostics"
	_, section, found := strings.Cut(string(data), "\n"+heading+"\n")
	if !found {
		t.Fatalf("docs/tooling.md has no %q section", heading)
	}
	if end := strings.Index(section, "\n#"); end >= 0 {
		section = section[:end]
	}
	row := regexp.MustCompile(`(?m)^\| (EF[0-9]+) \|`)
	var documented []string
	for _, match := range row.FindAllStringSubmatch(section, -1) {
		documented = append(documented, match[1])
	}
	declared := slices.Clone(Codes)
	slices.Sort(declared)
	slices.Sort(documented)
	if !slices.Equal(declared, documented) {
		t.Fatalf("internal/edition declares %v; the docs/tooling.md edition table documents %v", declared, documented)
	}
	if diagnostic := Unavailable("ergo lint FILE", E2026, TicketTooling); diagnostic.Code != CodeUnavailable || !strings.Contains(diagnostic.Message, `"2026"`) || !strings.Contains(diagnostic.Message, "#203") {
		t.Fatalf("EF145 message %q does not name the edition and the ticket", diagnostic.Message)
	}
}
