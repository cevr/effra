package main

// Controls for the pinned, reference-only Effect upstream submodule and its manifest.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

var fixtureGitConfig = []string{
	"-c", "commit.gpgsign=false", "-c", "core.hooksPath=" + os.DevNull, "-c", "init.templateDir=",
	"-c", "protocol.file.allow=always", "-c", "user.name=Snapshot Test", "-c", "user.email=test@example.invalid",
}

// hermeticGitEnv is the fixture environment: the process environment without
// global or system git configuration.
func hermeticGitEnv(extra ...string) []string {
	return append(os.Environ(), append([]string{"GIT_CONFIG_GLOBAL=" + os.DevNull, "GIT_CONFIG_NOSYSTEM=1"}, extra...)...)
}

func fixtureGit(t *testing.T, repository string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append(append(slices.Clone(fixtureGitConfig), "-C", repository), args...)...)
	command.Env = hermeticGitEnv()
	output, err := command.Output()
	if err != nil {
		stderr := ""
		if exit, ok := err.(*exec.ExitError); ok {
			stderr = string(exit.Stderr)
		}
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, stderr)
	}
	return strings.TrimSpace(string(output))
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for relative, contents := range files {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// corpusFixture is a fixture superproject whose submodule pins a fixture upstream commit.
type corpusFixture struct {
	root, upstream, effra, checkout string
	commit, later, tag              string
}

func newCorpusFixture(t *testing.T) *corpusFixture {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &corpusFixture{root: root, upstream: filepath.Join(root, "upstream"), effra: filepath.Join(root, "effra")}
	writeFiles(t, f.upstream, map[string]string{
		"LICENSE":                          "root-license\n",
		"packages/effect/LICENSE":          "effect-license\n",
		"packages/effect/src/Effect.ts":    "export const source = 0\n",
		"packages/effect/test/one.test.ts": "export const one = 1\n",
		"packages/other/test/two.test.ts":  "export const two = 2\n",
	})
	fixtureGit(t, root, "init", "-q", f.upstream)
	fixtureGit(t, f.upstream, "add", ".")
	fixtureGit(t, f.upstream, "commit", "-qm", "pinned")
	f.commit = fixtureGit(t, f.upstream, "rev-parse", "HEAD")
	writeFiles(t, f.upstream, map[string]string{"packages/other/test/two.test.ts": "export const two = 22\n"})
	fixtureGit(t, f.upstream, "commit", "-qam", "later")
	f.later = fixtureGit(t, f.upstream, "rev-parse", "HEAD")
	f.tag = "custom:" + f.commit

	fixtureGit(t, root, "init", "-q", f.effra)
	script, err := os.ReadFile(filepath.Join(repoRoot(t), initScript))
	if err != nil {
		t.Fatal(err)
	}
	writeFiles(t, f.effra, map[string]string{initScript: string(script)})
	if err := os.Chmod(filepath.Join(f.effra, initScript), 0o755); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, f.effra, "submodule", "add", "-q", f.upstream, checkoutRelative)
	f.checkout = filepath.Join(f.effra, checkoutRelative)
	fixtureGit(t, f.checkout, "checkout", "-q", "--detach", f.commit)
	// Only an explicit mirror can satisfy initialization; the recorded URL does not exist.
	fixtureGit(t, f.effra, "config", "-f", ".gitmodules", "submodule."+checkoutRelative+".url", "file:///nonexistent/effect.git")
	fixtureGit(t, f.effra, "add", ".")
	fixtureGit(t, f.effra, "commit", "-qm", "pin upstream")
	if _, err := refresh(f.effra, f.commit, f.tag); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, f.effra, "add", manifestRelative)
	fixtureGit(t, f.effra, "commit", "-qm", "record manifest")
	return f
}

func (f *corpusFixture) verify(t *testing.T) map[string]any {
	t.Helper()
	corpus, err := verify(f.effra, f.commit, f.tag)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	return corpus.manifest
}

func assertContains(t *testing.T, text string, fragments ...string) {
	t.Helper()
	for _, fragment := range fragments {
		if !strings.Contains(text, fragment) {
			t.Fatalf("%q\ndoes not contain\n%q", text, fragment)
		}
	}
}

func (f *corpusFixture) assertRefused(t *testing.T, fragments ...string) string {
	t.Helper()
	_, err := verify(f.effra, f.commit, f.tag)
	if err == nil {
		t.Fatalf("verify accepted the checkout; want a refusal containing %q", fragments)
	}
	assertContains(t, err.Error(), fragments...)
	return err.Error()
}

// runPrintedRepair runs each printed repair command from outside the Effra
// checkout, as a user passing --root would.
func (f *corpusFixture) runPrintedRepair(t *testing.T, message, mirror string) {
	t.Helper()
	repairs := message[strings.LastIndex(message, "; run ")+len("; run "):]
	for _, repair := range strings.Split(repairs, ", then ") {
		command := exec.Command("sh", "-c", repair)
		command.Dir = f.root
		command.Env = hermeticGitEnv("EFFRA_UPSTREAM_MIRROR=" + mirror)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("repair %q: %v\n%s", repair, err, output)
		}
	}
}

// editHiddenFromStatus edits a selected file that index flags hide; git status
// alone would call the checkout clean.
func (f *corpusFixture) editHiddenFromStatus(t *testing.T, target string) {
	t.Helper()
	writeFiles(t, f.checkout, map[string]string{target: "edited behind git status\n"})
	if status := fixtureGit(t, f.checkout, "status", "--porcelain", "--untracked-files=no"); status != "" {
		t.Fatalf("git status shows the hidden edit: %q", status)
	}
}

func (f *corpusFixture) assertFlagRefusalThenRepair(t *testing.T, target string) {
	t.Helper()
	message := f.assertRefused(t,
		fmt.Sprintf("hides selected tracked files from git status with assume-unchanged or skip-worktree (1, first %s)", target),
		fmt.Sprintf("run git -C %s read-tree HEAD, then %s/scripts/init_upstream.sh --force", f.checkout, f.effra),
	)
	// Every integrity-bearing read comes from the pinned objects, never the edited file.
	corpus, err := readPinned(f.checkout, f.commit, f.tag)
	if err != nil {
		t.Fatal(err)
	}
	if text, err := corpus.text(target); err != nil || text != "export const one = 1\n" {
		t.Fatalf("pinned text = %q, %v", text, err)
	}
	// The printed repair clears the flags; only then can a forced checkout restore the file.
	f.runPrintedRepair(t, message, "")
	if data, _ := os.ReadFile(filepath.Join(f.checkout, target)); string(data) != "export const one = 1\n" {
		t.Fatalf("repaired file = %q", data)
	}
	f.verify(t)
}

const fixtureTarget = "packages/effect/test/one.test.ts"

func TestAssumeUnchangedEditIsRefusedAndNeverRead(t *testing.T) {
	t.Parallel()
	f := newCorpusFixture(t)
	fixtureGit(t, f.checkout, "update-index", "--assume-unchanged", "--", fixtureTarget)
	f.editHiddenFromStatus(t, fixtureTarget)
	f.assertFlagRefusalThenRepair(t, fixtureTarget)
}

func TestSkipWorktreeEditIsRefusedAndNeverRead(t *testing.T) {
	t.Parallel()
	f := newCorpusFixture(t)
	fixtureGit(t, f.checkout, "update-index", "--skip-worktree", "--", fixtureTarget)
	f.editHiddenFromStatus(t, fixtureTarget)
	f.assertFlagRefusalThenRepair(t, fixtureTarget)
}

func TestIgnoreStatCheckoutIsRefusedAndNeverRead(t *testing.T) {
	t.Parallel()
	f := newCorpusFixture(t)
	fixtureGit(t, f.checkout, "config", "core.ignoreStat", "true")
	// With core.ignoreStat, git marks every file it writes assume-unchanged.
	if err := os.Remove(filepath.Join(f.checkout, fixtureTarget)); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, f.checkout, "checkout", "--", fixtureTarget)
	if listing := fixtureGit(t, f.checkout, "ls-files", "-v", "--", fixtureTarget); listing != "h "+fixtureTarget {
		t.Fatalf("ls-files -v = %q", listing)
	}
	f.editHiddenFromStatus(t, fixtureTarget)
	message := f.assertRefused(t,
		fmt.Sprintf("Effect upstream checkout %s enables core.ignoreStat, which hides edits from git status", checkoutRelative),
		fmt.Sprintf("run git -C %s config core.ignoreStat false", f.checkout),
	)
	f.runPrintedRepair(t, message, "")
	f.assertFlagRefusalThenRepair(t, fixtureTarget)
}

// This test changes the process environment, so it cannot run in parallel.
func TestUnparseableIgnoreStatNamesEveryOriginAndThePrintedRepairRestoresIt(t *testing.T) {
	f := newCorpusFixture(t)
	// git refuses to open the checkout while any core.ignoreStat entry is not a boolean; initializing cannot help.
	settings := filepath.Join(f.root, "global.gitconfig")
	writeFiles(t, f.root, map[string]string{"global.gitconfig": "[core]\n\tignoreStat = maybe\n"})
	fixtureGit(t, f.checkout, "config", "core.ignoreStat", "sometimes")
	local := filepath.Join(f.effra, ".git", "modules", checkoutRelative, "config")
	t.Setenv("GIT_CONFIG_GLOBAL", settings)
	message := f.assertRefused(t,
		fmt.Sprintf("Effect upstream checkout %s cannot be opened because git rejects its core.ignoreStat setting ", checkoutRelative)+
			"(fatal: bad boolean config value 'maybe' for 'core.ignorestat')",
		fmt.Sprintf("run git config --file %s --replace-all core.ignoreStat false, then git config --file %s --replace-all core.ignoreStat false", settings, local),
	)
	if strings.Contains(message, "not initialized") {
		t.Fatalf("refusal names initialization: %q", message)
	}
	f.runPrintedRepair(t, message, "")
	f.verify(t)
	t.Run("command line configuration", func(t *testing.T) {
		t.Setenv("GIT_CONFIG_PARAMETERS", "'core.ignorestat'='maybe'")
		f.assertRefused(t, "; remove core.ignoreStat from the command line configuration")
	})
}

func TestRefreshRecordsSelectionAndNearestLicenses(t *testing.T) {
	t.Parallel()
	f := newCorpusFixture(t)
	manifest := f.verify(t)
	if count := object(manifest, "selection")["count"]; !isPyInt(count, 2) {
		t.Fatalf("selection count = %v", count)
	}
	mapping := object(object(manifest, "licenses"), "mapping")
	want := map[string]any{
		"packages/effect/test/one.test.ts": "packages/effect/LICENSE",
		"packages/other/test/two.test.ts":  "LICENSE",
	}
	if _, differs := firstDifference(mapping, want, ""); differs || len(mapping) != len(want) {
		t.Fatalf("license mapping = %v", mapping)
	}
	var paths []string
	for _, item := range manifest["files"].([]any) {
		paths = append(paths, item.(map[string]any)["path"].(string))
	}
	if want := []string{"LICENSE", "packages/effect/LICENSE", "packages/effect/test/one.test.ts", "packages/other/test/two.test.ts"}; !slices.Equal(paths, want) {
		t.Fatalf("file paths = %v", paths)
	}
	if kind := object(manifest, "provenance")["kind"]; kind != "custom-self-consistent" {
		t.Fatalf("provenance kind = %v", kind)
	}
	if err := validateReleaseIdentity(manifest); err == nil {
		t.Fatal("a custom manifest passed as the release identity")
	}
}

func TestUninitializedCheckoutNamesInitAndOfflineMirrorRestoresIt(t *testing.T) {
	t.Parallel()
	f := newCorpusFixture(t)
	fixtureGit(t, f.effra, "submodule", "deinit", "-q", "-f", checkoutRelative)
	if err := os.RemoveAll(filepath.Join(f.effra, ".git", "modules")); err != nil {
		t.Fatal(err)
	}
	f.assertRefused(t, fmt.Sprintf("is not initialized; run %s/scripts/init_upstream.sh", f.effra))
	// Run from outside the repository: the printed repair must not depend on the current directory.
	code, _, stderr := runCLI(t, f.root, hermeticGitEnv(), "import", "--root", f.effra)
	if code == 0 {
		t.Fatal("import accepted an uninitialized checkout")
	}
	want := fmt.Sprintf("import_effect_conformance: Effect upstream checkout %s is not initialized; run %s/scripts/init_upstream.sh\n", checkoutRelative, f.effra)
	if stderr != want {
		t.Fatalf("stderr = %q, want %q", stderr, want)
	}

	f.runPrintedRepair(t, strings.TrimSpace(stderr), f.upstream)
	f.verify(t)
	if shallow := fixtureGit(t, f.checkout, "rev-parse", "--is-shallow-repository"); shallow != "true" {
		t.Fatalf("restored checkout is not shallow: %q", shallow)
	}
	if status := fixtureGit(t, f.effra, "status", "--porcelain"); status != "" {
		t.Fatalf("superproject status = %q", status)
	}
}

func TestCheckoutAtAnotherCommitNamesInitWhichRestoresThePin(t *testing.T) {
	t.Parallel()
	f := newCorpusFixture(t)
	fixtureGit(t, f.checkout, "checkout", "-q", "--detach", f.later)
	message := f.assertRefused(t, fmt.Sprintf("is at %s, not the pinned commit %s; run %s/scripts/init_upstream.sh", f.later, f.commit, f.effra))
	f.runPrintedRepair(t, message, "")
	f.verify(t)
}

func TestModifiedTrackedFileIsRefusedEvenWithARehashedManifest(t *testing.T) {
	t.Parallel()
	f := newCorpusFixture(t)
	changed := "changed and rehashed\n"
	writeFiles(t, f.checkout, map[string]string{fixtureTarget: changed})
	manifestPath := filepath.Join(f.effra, manifestRelative)
	manifest := readJSONObject(t, manifestPath)
	for _, value := range manifest["files"].([]any) {
		if entry := value.(map[string]any); entry["path"] == fixtureTarget {
			entry["bytes"] = number(len(changed))
			entry["sha256"] = sha256Hex([]byte(changed))
		}
	}
	manifest["integrity"] = integrityForManifest(manifest)
	writeFiles(t, f.effra, map[string]string{manifestRelative: renderManifest(manifest)})
	message := f.assertRefused(t, "has modified tracked files", fmt.Sprintf("run %s/scripts/init_upstream.sh --force", f.effra))
	f.runPrintedRepair(t, message, "")
	// The checkout is restored; hashes still come from the pinned objects.
	f.assertRefused(t, "manifest differs from the pinned checkout at /files[packages/effect/test/one.test.ts]/sha256")
}

func TestGitlinkMustRecordThePinnedCommit(t *testing.T) {
	t.Parallel()
	f := newCorpusFixture(t)
	_, err := verify(f.effra, f.later, "custom:"+f.later)
	if err == nil {
		t.Fatal("verify accepted a commit the gitlink does not record")
	}
	assertContains(t, err.Error(), fmt.Sprintf("records %s, not the pinned commit %s", f.commit, f.later))
}

func readJSONObject(t *testing.T, path string) map[string]any {
	t.Helper()
	text, err := readText(path)
	if err != nil {
		t.Fatal(err)
	}
	value, err := decodeJSON(text)
	if err != nil {
		t.Fatal(err)
	}
	return value.(map[string]any)
}

func TestManifestMustEqualTheRecomputedCanonicalManifest(t *testing.T) {
	t.Parallel()
	f := newCorpusFixture(t)
	manifestPath := filepath.Join(f.effra, manifestRelative)
	original, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct {
		location string
		mutate   func(map[string]any)
	}{
		{"/selection/count", func(value map[string]any) { object(value, "selection")["count"] = number(3) }},
		{"/files[length]", func(value map[string]any) {
			files := value["files"].([]any)
			first := map[string]any{}
			for key, item := range files[0].(map[string]any) {
				first[key] = item
			}
			value["files"] = append(files, first)
		}},
		{"/licenses/mapping/packages/other/test/two.test.ts", func(value map[string]any) {
			object(object(value, "licenses"), "mapping")["packages/other/test/two.test.ts"] = "packages/effect/LICENSE"
		}},
		{"/source/tag", func(value map[string]any) { object(value, "source")["tag"] = "effect@4.0.1" }},
		{"/status", func(value map[string]any) { value["status"] = "passing" }},
	} {
		t.Run(mutation.location, func(t *testing.T) {
			manifest, err := decodeJSON(string(original))
			if err != nil {
				t.Fatal(err)
			}
			mutation.mutate(manifest.(map[string]any))
			writeFiles(t, f.effra, map[string]string{manifestRelative: renderManifest(manifest)})
			f.assertRefused(t, "manifest differs from the pinned checkout at "+mutation.location)
		})
	}
	var value any
	if err := json.Unmarshal(original, &value); err != nil {
		t.Fatal(err)
	}
	compact, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	writeFiles(t, f.effra, map[string]string{manifestRelative: string(compact)})
	f.assertRefused(t, "manifest is not in canonical form")
	for _, contents := range []string{"\xff", strings.Repeat("[", 100000) + "0" + strings.Repeat("]", 100000)} {
		writeFiles(t, f.effra, map[string]string{manifestRelative: contents})
		f.assertRefused(t, "manifest is not readable UTF-8 JSON")
	}
}

func TestMissingGitIsAStructuredCLIRefusal(t *testing.T) {
	t.Parallel()
	f := newCorpusFixture(t)
	code, _, stderr := runCLI(t, f.root, hermeticGitEnv("PATH="+t.TempDir()), "import", "--root", f.effra)
	if code == 0 {
		t.Fatal("import succeeded without git")
	}
	assertContains(t, stderr, "import_effect_conformance: cannot execute git:")
	if strings.Contains(stderr, "panic") || strings.Contains(stderr, "goroutine") {
		t.Fatalf("unstructured failure: %s", stderr)
	}
}

// releaseCorpus verifies the repository's own submodule at the Effect 4.0.1
// release pin. Go's test cache cannot see files that only git reads, so it
// also stats the checkout's selected files and index state in-process.
func releaseCorpus(t *testing.T) pinnedCorpus {
	t.Helper()
	root := repoRoot(t)
	corpus, err := verify(root, pinnedCommit, pinnedTag)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	checkout := filepath.Join(root, checkoutRelative)
	for path := range corpus.contents {
		os.Stat(filepath.Join(checkout, path))
	}
	if link, err := os.ReadFile(filepath.Join(checkout, ".git")); err == nil {
		gitdir := strings.TrimSpace(strings.TrimPrefix(string(link), "gitdir:"))
		if !filepath.IsAbs(gitdir) {
			gitdir = filepath.Join(checkout, gitdir)
		}
		for _, name := range []string{"HEAD", "index", "config"} {
			os.Stat(filepath.Join(gitdir, name))
		}
	}
	return corpus
}

func TestCommittedManifestIsExactlyThePinnedRelease(t *testing.T) {
	t.Parallel()
	manifest := releaseCorpus(t).manifest
	source, integrity := object(manifest, "source"), object(manifest, "integrity")
	if source["commit"] != pinnedCommit || source["tag"] != pinnedTag {
		t.Fatalf("source = %v", source)
	}
	if count := object(manifest, "selection")["count"]; !isPyInt(count, 746) {
		t.Fatalf("selection count = %v", count)
	}
	if paths := object(manifest, "licenses")["paths"].([]any); len(paths) != 26 {
		t.Fatalf("license paths = %d", len(paths))
	}
	if integrity["rootSha256"] != releaseRootIdentitySHA256 || integrity["referenceIdentitySha256"] != releaseReferenceIdentitySHA256 {
		t.Fatalf("integrity = %v", integrity)
	}
	if entry := strings.Fields(fixtureGit(t, repoRoot(t), "ls-files", "--stage", "--", checkoutRelative)); len(entry) < 2 || entry[0] != "160000" || entry[1] != pinnedCommit {
		t.Fatalf("gitlink = %v", entry)
	}
}

func TestRehashedManifestEntryIsRejected(t *testing.T) {
	t.Parallel()
	corpus := releaseCorpus(t)
	manifest, err := decodeJSON(string(canonicalJSON(corpus.manifest)))
	if err != nil {
		t.Fatal(err)
	}
	const target = "packages/ai/anthropic/test/AnthropicClient.test.ts"
	for _, value := range manifest.(map[string]any)["files"].([]any) {
		if entry := value.(map[string]any); entry["path"] == target {
			entry["sha256"] = sha256Hex([]byte("fully changed payload with rehashed metadata\n"))
		}
	}
	manifest.(map[string]any)["integrity"] = integrityForManifest(manifest.(map[string]any))
	path := filepath.Join(t.TempDir(), "manifest.json")
	if err := os.WriteFile(path, []byte(renderManifest(manifest)), 0o644); err != nil {
		t.Fatal(err)
	}
	pinned, err := readPinned(filepath.Join(repoRoot(t), checkoutRelative), pinnedCommit, pinnedTag)
	if err != nil {
		t.Fatal(err)
	}
	err = verifyManifest(pinned.manifest, path, pinnedCommit, pinnedTag)
	if err == nil {
		t.Fatal("a rehashed manifest entry was accepted")
	}
	assertContains(t, err.Error(), "/files["+target+"]/sha256")
}

func TestIndependentReleaseIdentityPinsTheSelectionRule(t *testing.T) {
	t.Parallel()
	releaseCorpus(t)
	components := slices.DeleteFunc(slices.Clone(selectionComponents), func(component string) bool { return component == "typetest" })
	changed, err := readPinnedSelecting(filepath.Join(repoRoot(t), checkoutRelative), pinnedCommit, pinnedTag, components)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateReleaseIdentity(changed.manifest); err == nil {
		t.Fatal("a changed selection rule kept the release identity")
	}
}
