package main

// Verify or refresh the pinned Effect test corpus used as reference-only
// conformance data.
//
// The upstream bytes live in the git submodule at conformance/upstream/effect,
// pinned by its gitlink. Effra owns only the manifest: the selection rule, the
// license accounting and the sha256 identity of every selected file.
// Verification requires the checkout at the pinned commit with no tracked
// modifications and no index flag that hides one, recomputes the manifest from
// the commit's immutable git objects and compares it with the committed
// manifest. Consumers read selected files only from those objects, never from
// the checkout's working files. These files describe upstream behavior; they
// are not claims that Effra executes or passes the upstream suite.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	pinnedCommit                   = "460272d30457f4697d8b8c52cad41caccbcace08"
	pinnedTag                      = "effect@4.0.1"
	canonicalEncoding              = "json-sorted-compact-utf8-v1"
	releaseReferenceCount          = 746
	releaseLicenseCount            = 26
	releaseReferenceIdentitySHA256 = "9b9a09038a2b3d5e15256aed9d37401a06e911569d8fcc4ac3f1368fbbe8e001"
	releaseLicenseIdentitySHA256   = "5d9c07de330f6f9de1cdf0306d7c25fde646691edcaba92fd740fdeeb1663b90"
	releaseRootIdentitySHA256      = "48f6287ce974209b0860671f0e62b0ef0a2f5436836f7e7c2aa935c4eb95c422"
	checkoutRelative               = "conformance/upstream/effect"
	manifestRelative               = "conformance/effect-upstream.manifest.json"
	initScript                     = "scripts/init_upstream.sh"
)

// selectionComponents are the path components that select an upstream file.
var selectionComponents = []string{"test", "tests", "typetest", "test-dts"}

var licenseNames = []string{"LICENSE", "LICENCE", "COPYING", "LICENSE.md", "LICENCE.md", "COPYING.md"}

var regularFileModes = map[string]bool{"100644": true, "100755": true}

// gitResult is a finished git process.
type gitResult struct {
	stdout, stderr []byte
	code           int
}

// gitProcess runs git in a repository; it fails only when git cannot run.
func gitProcess(repository string, stdin []byte, args ...string) (gitResult, error) {
	command := exec.Command("git", append([]string{"-C", repository}, args...)...)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if stdin != nil {
		command.Stdin = bytes.NewReader(stdin)
	}
	err := command.Run()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return gitResult{}, fmt.Errorf("cannot execute git: %v", err)
	}
	return gitResult{stdout.Bytes(), stderr.Bytes(), command.ProcessState.ExitCode()}, nil
}

func runGit(repository string, args ...string) (string, error) {
	result, err := gitProcess(repository, nil, args...)
	if err != nil {
		return "", err
	}
	if result.code != 0 {
		return "", fmt.Errorf("git %s failed: %s", strings.Join(args, " "), pyStrip(string(result.stderr)))
	}
	return string(result.stdout), nil
}

// pathParts is pathlib's PurePosixPath(value).parts for a relative path:
// empty and "." segments collapse away.
func pathParts(value string) []string {
	var parts []string
	for _, part := range strings.Split(value, "/") {
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	return parts
}

// safeRelativePath accepts a nonempty relative path with no parent segment.
// Like the pathlib original, "." segments collapse and are accepted.
func safeRelativePath(value any) (string, error) {
	text, ok := value.(string)
	if !ok || text == "" || strings.ContainsAny(text, "\\\x00\n") || strings.HasPrefix(text, "/") || slices.Contains(pathParts(text), "..") {
		return "", fmt.Errorf("unsafe upstream path: %s", pyRepr(value))
	}
	return text, nil
}

func treeModes(checkout, commit string) (map[string]string, error) {
	listing, err := runGit(checkout, "ls-tree", "-r", "-z", commit)
	if err != nil {
		return nil, err
	}
	modes := map[string]string{}
	for _, entry := range strings.Split(listing, "\x00") {
		if entry == "" {
			continue
		}
		header, path, _ := strings.Cut(entry, "\t")
		mode, _, _ := strings.Cut(header, " ")
		modes[path] = mode
	}
	return modes, nil
}

func selectedPaths(tracked map[string]string, components []string) []string {
	var paths []string
	for path := range tracked {
		for _, part := range pathParts(path) {
			if slices.Contains(components, part) {
				paths = append(paths, path)
				break
			}
		}
	}
	sort.Strings(paths)
	return paths
}

func nearestLicense(tracked map[string]string, relative string) (string, error) {
	parts := pathParts(relative)
	for depth := len(parts) - 1; depth > 0; depth-- {
		directory := strings.Join(parts[:depth], "/")
		for _, name := range licenseNames {
			if candidate := directory + "/" + name; tracked[candidate] != "" {
				return candidate, nil
			}
		}
	}
	for _, name := range licenseNames {
		if tracked[name] != "" {
			return name, nil
		}
	}
	return "", fmt.Errorf("no applicable license for selected file: %s", relative)
}

func licenseMap(tracked map[string]string, paths []string) (map[string]string, error) {
	result := map[string]string{}
	for _, relative := range paths {
		license, err := nearestLicense(tracked, relative)
		if err != nil {
			return nil, err
		}
		result[relative] = license
	}
	for _, license := range result {
		result[license] = license
	}
	return result, nil
}

// sortLicenseFirst orders paths with the root LICENSE first, then by code point.
func sortLicenseFirst(paths []string) {
	sort.Slice(paths, func(i, j int) bool {
		if (paths[i] == "LICENSE") != (paths[j] == "LICENSE") {
			return paths[i] == "LICENSE"
		}
		return paths[i] < paths[j]
	})
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func canonicalSHA256(value any) string {
	return sha256Hex(canonicalJSON(value))
}

func number(value int) json.Number {
	return json.Number(strconv.Itoa(value))
}

// object returns value[key] as an object; a missing or non-object field reads as empty.
func object(value map[string]any, key string) map[string]any {
	result, _ := value[key].(map[string]any)
	return result
}

func manifestIdentity(manifest map[string]any) map[string]any {
	source, selection := object(manifest, "source"), object(manifest, "selection")
	files, _ := manifest["files"].([]any)
	references, licenses := []any{}, []any{}
	for _, value := range files {
		item, ok := value.(map[string]any)
		if !ok {
			continue
		}
		switch item["kind"] {
		case "reference":
			references = append(references, map[string]any{"path": item["path"], "license": item["license"], "bytes": item["bytes"], "sha256": item["sha256"]})
		case "license":
			licenses = append(licenses, map[string]any{"path": item["path"], "bytes": item["bytes"], "sha256": item["sha256"]})
		}
	}
	return map[string]any{
		"schemaVersion": manifest["schemaVersion"],
		"source": map[string]any{
			"repository": source["repository"],
			"tag":        source["tag"],
			"commit":     source["commit"],
			"url":        source["url"],
		},
		"selection": map[string]any{
			"kind":       selection["kind"],
			"components": selection["components"],
			"count":      selection["count"],
		},
		"references": references,
		"licenses":   licenses,
	}
}

func integrityForManifest(manifest map[string]any) map[string]any {
	identity := manifestIdentity(manifest)
	return map[string]any{
		"algorithm":               "sha256",
		"canonicalEncoding":       canonicalEncoding,
		"referenceIdentitySha256": canonicalSHA256(identity["references"]),
		"licenseIdentitySha256":   canonicalSHA256(identity["licenses"]),
		"rootSha256":              canonicalSHA256(identity),
	}
}

// blobBytes reads each path from the commit's immutable objects with one git process.
func blobBytes(checkout, commit string, paths []string) (map[string][]byte, error) {
	var request strings.Builder
	for _, path := range paths {
		safe, err := safeRelativePath(path)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&request, "%s:%s\n", commit, safe)
	}
	result, err := gitProcess(checkout, []byte(request.String()), "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	if result.code != 0 {
		return nil, fmt.Errorf("git cat-file failed: %s", pyStrip(strings.ToValidUTF8(string(result.stderr), "�")))
	}
	output := result.stdout
	blobs := map[string][]byte{}
	offset := 0
	for _, path := range paths {
		end := bytes.IndexByte(output[offset:], '\n')
		if end < 0 {
			return nil, fmt.Errorf("git cat-file output ends before %s", path)
		}
		end += offset
		header := strings.Fields(string(output[offset:end]))
		if len(header) != 3 || header[1] != "blob" {
			return nil, fmt.Errorf("pinned tree has no file at %s", path)
		}
		size, err := strconv.Atoi(header[2])
		if err != nil || end+1+size > len(output) {
			return nil, fmt.Errorf("git cat-file output is truncated at %s", path)
		}
		blobs[path] = output[end+1 : end+1+size]
		offset = end + 2 + size
	}
	return blobs, nil
}

// pinnedCorpus is a commit's recomputed manifest and the exact selected bytes
// it hashes, read from git objects. It is the only source of selected upstream
// contents for consumers: the checkout's working files can differ from the
// pinned commit behind index flags.
type pinnedCorpus struct {
	manifest map[string]any
	contents map[string][]byte
}

func (corpus pinnedCorpus) text(path string) (string, error) {
	contents, ok := corpus.contents[path]
	if !ok {
		return "", fmt.Errorf("not a selected upstream file: %s", path)
	}
	if !utf8.Valid(contents) {
		return "", fmt.Errorf("selected upstream file is not UTF-8: %s", path)
	}
	return string(contents), nil
}

// readPinned recomputes the manifest of a commit from git objects, never the working tree.
func readPinned(checkout, commit, tag string) (pinnedCorpus, error) {
	return readPinnedSelecting(checkout, commit, tag, selectionComponents)
}

func readPinnedSelecting(checkout, commit, tag string, components []string) (pinnedCorpus, error) {
	tracked, err := treeModes(checkout, commit)
	if err != nil {
		return pinnedCorpus{}, err
	}
	paths := selectedPaths(tracked, components)
	if len(paths) == 0 {
		return pinnedCorpus{}, errors.New("upstream selection is empty")
	}
	licenses, err := licenseMap(tracked, paths)
	if err != nil {
		return pinnedCorpus{}, err
	}
	isLicense := map[string]bool{}
	for _, license := range licenses {
		isLicense[license] = true
	}
	var contentPaths, licensePaths []string
	for path := range licenses {
		contentPaths = append(contentPaths, path)
	}
	for path := range isLicense {
		licensePaths = append(licensePaths, path)
	}
	sortLicenseFirst(contentPaths)
	sortLicenseFirst(licensePaths)
	for _, relative := range contentPaths {
		if !regularFileModes[tracked[relative]] {
			return pinnedCorpus{}, fmt.Errorf("selected upstream entry is not a regular file: %s", relative)
		}
	}
	blobs, err := blobBytes(checkout, commit, contentPaths)
	if err != nil {
		return pinnedCorpus{}, err
	}
	files := make([]any, 0, len(contentPaths))
	for _, relative := range contentPaths {
		kind := "reference"
		if isLicense[relative] {
			kind = "license"
		}
		files = append(files, map[string]any{
			"path":    relative,
			"kind":    kind,
			"license": licenses[relative],
			"bytes":   number(len(blobs[relative])),
			"sha256":  sha256Hex(blobs[relative]),
		})
	}
	mapping := map[string]any{}
	for _, relative := range paths {
		mapping[relative] = licenses[relative]
	}
	sortedComponents := slices.Clone(components)
	sort.Strings(sortedComponents)
	provenance := "custom-self-consistent"
	if commit == pinnedCommit && tag == pinnedTag {
		provenance = "release-pinned"
	}
	manifest := map[string]any{
		"schemaVersion": number(2),
		"status":        "reference-only-not-executed",
		"source": map[string]any{
			"repository": "effect-ts/effect",
			"url":        "https://github.com/Effect-TS/effect/tree/" + commit,
			"commit":     commit,
			"tag":        tag,
		},
		"selection": map[string]any{
			"kind":       "tracked-files-with-path-component",
			"components": stringList(sortedComponents),
			"count":      number(len(paths)),
		},
		"licenses": map[string]any{
			"paths":   stringList(licensePaths),
			"mapping": mapping,
		},
		"files": files,
		"provenance": map[string]any{
			"kind":              provenance,
			"canonicalEncoding": canonicalEncoding,
		},
	}
	manifest["integrity"] = integrityForManifest(manifest)
	return pinnedCorpus{manifest, blobs}, nil
}

func stringList(values []string) []any {
	result := make([]any, len(values))
	for i, value := range values {
		result[i] = value
	}
	return result
}

func renderManifest(manifest any) string {
	return indentedJSON(manifest) + "\n"
}

// firstDifference returns the location of the first difference between two
// json.loads values, labelling list items by the expected item's path.
func firstDifference(actual, expected any, location string) (string, bool) {
	actualObject, actualIsObject := actual.(map[string]any)
	expectedObject, expectedIsObject := expected.(map[string]any)
	if actualIsObject && expectedIsObject {
		keys := map[string]any{}
		for key := range actualObject {
			keys[key] = nil
		}
		for key := range expectedObject {
			keys[key] = nil
		}
		for _, key := range sortedKeys(keys) {
			left, inActual := actualObject[key]
			right, inExpected := expectedObject[key]
			if !inActual || !inExpected {
				return location + "/" + key, true
			}
			if found, ok := firstDifference(left, right, location+"/"+key); ok {
				return found, true
			}
		}
		return "", false
	}
	actualList, actualIsList := actual.([]any)
	expectedList, expectedIsList := expected.([]any)
	if actualIsList && expectedIsList {
		for index := 0; index < min(len(actualList), len(expectedList)); index++ {
			label := strconv.Itoa(index)
			if item, ok := expectedList[index].(map[string]any); ok {
				if path, ok := item["path"]; ok {
					label = pyStr(path)
				}
			}
			if found, ok := firstDifference(actualList[index], expectedList[index], location+"["+label+"]"); ok {
				return found, true
			}
		}
		if len(actualList) == len(expectedList) {
			return "", false
		}
		return location + "[length]", true
	}
	if pyType(actual) != pyType(expected) || !pyScalarEqual(actual, expected) {
		if location == "" {
			return "/", true
		}
		return location, true
	}
	return "", false
}

// pyStr is Python's str() of a json.loads value.
func pyStr(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return pyRepr(value)
}

// resolvePath is pathlib's non-strict Path.resolve: absolute, with every
// existing symlink resolved.
func resolvePath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = filepath.Clean(path)
	}
	if real, err := filepath.EvalSymlinks(absolute); err == nil {
		return real
	}
	parent := filepath.Dir(absolute)
	if parent == absolute {
		return absolute
	}
	return filepath.Join(resolvePath(parent), filepath.Base(absolute))
}

// initCommand names the init script by absolute path, so a repair printed for
// --root runs from any directory.
func initCommand(root string, args ...string) string {
	return shlexJoin(append([]string{resolvePath(filepath.Join(root, initScript))}, args...)...)
}

// checkoutGitCommand names a git command on the checkout by absolute path, so
// it runs from any directory.
func checkoutGitCommand(root string, args ...string) string {
	return shlexJoin(append([]string{"git", "-C", resolvePath(filepath.Join(root, checkoutRelative))}, args...)...)
}

// unopenableCheckout explains why git cannot open an existing checkout
// directory, naming the repair that applies.
func unopenableCheckout(root, checkout string, failure gitResult) error {
	detail := pyStrip(string(failure.stderr))
	// git refuses to open a repository while any core.ignoreStat entry, in any scope, is not a boolean.
	ignoreStat, err := gitProcess(checkout, nil, "config", "--type=bool", "core.ignoreStat")
	if err != nil {
		return err
	}
	if ignoreStat.code != 0 && ignoreStat.code != 1 {
		listing, err := gitProcess(checkout, nil, "config", "--show-origin", "-z", "--get-all", "core.ignoreStat")
		if err != nil {
			return err
		}
		var fields []string
		if listing.code == 0 {
			fields = strings.Split(string(listing.stdout), "\x00")
			fields = fields[:len(fields)-1]
		}
		var commands, others []string
		seen := map[string]bool{}
		for i := 0; i < len(fields); i += 2 {
			origin := fields[i]
			if seen[origin] {
				continue
			}
			seen[origin] = true
			kind, location, _ := strings.Cut(origin, ":")
			if kind == "file" {
				if !filepath.IsAbs(location) {
					location = filepath.Join(checkout, location)
				}
				commands = append(commands, shlexJoin("git", "config", "--file", resolvePath(location), "--replace-all", "core.ignoreStat", "false"))
			} else {
				others = append(others, kind)
			}
		}
		var repairs []string
		if len(commands) > 0 {
			repairs = append(repairs, "run "+strings.Join(commands, ", then "))
		}
		for _, kind := range others {
			repairs = append(repairs, fmt.Sprintf("remove core.ignoreStat from the %s configuration", kind))
		}
		if len(repairs) > 0 {
			return fmt.Errorf("Effect upstream checkout %s cannot be opened because git rejects its core.ignoreStat setting (%s); %s", checkoutRelative, detail, strings.Join(repairs, "; "))
		}
	}
	return fmt.Errorf("Effect upstream checkout %s is not initialized (%s); run %s", checkoutRelative, detail, initCommand(root))
}

// pinnedCheckout returns the submodule checkout, failing closed unless it is
// exactly the pinned commit.
func pinnedCheckout(root, commit string) (string, error) {
	checkout := filepath.Join(root, checkoutRelative)
	var toplevel *gitResult
	if info, err := os.Stat(checkout); err == nil && info.IsDir() {
		result, err := gitProcess(checkout, nil, "rev-parse", "--show-toplevel")
		if err != nil {
			return "", err
		}
		if result.code != 0 {
			return "", unopenableCheckout(root, checkout, result)
		}
		toplevel = &result
	}
	if toplevel == nil || filepath.Clean(pyStrip(string(toplevel.stdout))) != resolvePath(checkout) {
		return "", fmt.Errorf("Effect upstream checkout %s is not initialized; run %s", checkoutRelative, initCommand(root))
	}
	staged, err := runGit(root, "ls-files", "--stage", "--", checkoutRelative)
	if err != nil {
		return "", err
	}
	entry := strings.Fields(staged)
	if len(entry) < 2 || entry[0] != "160000" || entry[1] != commit {
		recorded := "no gitlink"
		if len(entry) > 1 && entry[0] == "160000" {
			recorded = entry[1]
		}
		return "", fmt.Errorf("%s records %s, not the pinned commit %s", checkoutRelative, recorded, commit)
	}
	head, err := runGit(checkout, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	if head = pyStrip(head); head != commit {
		return "", fmt.Errorf("Effect upstream checkout %s is at %s, not the pinned commit %s; run %s", checkoutRelative, head, commit, initCommand(root))
	}
	// core.ignoreStat marks every file git writes assume-unchanged, so the flag refusal below would recur after each repair.
	ignoreStat, err := gitProcess(checkout, nil, "config", "--type=bool", "core.ignoreStat")
	if err != nil {
		return "", err
	}
	if ignoreStat.code != 0 && ignoreStat.code != 1 {
		return "", fmt.Errorf("git config core.ignoreStat failed: %s", pyStrip(string(ignoreStat.stderr)))
	}
	if pyStrip(string(ignoreStat.stdout)) == "true" {
		return "", fmt.Errorf("Effect upstream checkout %s enables core.ignoreStat, which hides edits from git status; run %s", checkoutRelative, checkoutGitCommand(root, "config", "core.ignoreStat", "false"))
	}
	status, err := runGit(checkout, "status", "--porcelain=v1", "--untracked-files=no", "--ignore-submodules=all")
	if err != nil {
		return "", err
	}
	if modified := pySplitlines(status); len(modified) > 0 {
		return "", fmt.Errorf("Effect upstream checkout %s has modified tracked files (%s); run %s", checkoutRelative, pyStrip(modified[0]), initCommand(root, "--force"))
	}
	return checkout, nil
}

// refuseHiddenPaths refuses selected paths whose assume-unchanged or
// skip-worktree flag hides edits from git status.
func refuseHiddenPaths(root string, selected map[string][]byte) error {
	listing, err := runGit(filepath.Join(root, checkoutRelative), "ls-files", "-v", "-z")
	if err != nil {
		return err
	}
	var hidden []string
	for _, entry := range strings.Split(listing, "\x00") {
		tag, path, _ := strings.Cut(entry, " ")
		if _, ok := selected[path]; !ok {
			continue
		}
		// ls-files -v writes S for skip-worktree and a lowercase tag for assume-unchanged.
		lower := strings.ToLower(tag) == tag && strings.ToUpper(tag) != tag
		if lower || strings.ToUpper(tag) == "S" {
			hidden = append(hidden, path)
		}
	}
	if len(hidden) > 0 {
		// Rebuilding the index from HEAD drops every flag at once; update-index applies only one flag option per run.
		return fmt.Errorf(
			"Effect upstream checkout %s hides selected tracked files from git status with assume-unchanged or skip-worktree (%d, first %s); run %s, then %s",
			checkoutRelative, len(hidden), hidden[0], checkoutGitCommand(root, "read-tree", "HEAD"), initCommand(root, "--force"),
		)
	}
	return nil
}

// checkedOut reads the pinned corpus from git objects once the checkout
// passes every fail-closed check.
func checkedOut(root, commit, tag string) (pinnedCorpus, error) {
	checkout, err := pinnedCheckout(root, commit)
	if err != nil {
		return pinnedCorpus{}, err
	}
	corpus, err := readPinned(checkout, commit, tag)
	if err != nil {
		return pinnedCorpus{}, err
	}
	if err := refuseHiddenPaths(root, corpus.contents); err != nil {
		return pinnedCorpus{}, err
	}
	return corpus, nil
}

// isPyInt reports whether a json.loads value equals the integer n under
// Python's ==, as dict.get(...) != n compares it.
func isPyInt(value any, n int) bool {
	switch v := value.(type) {
	case json.Number:
		if i, ok := jsonInt(v); ok {
			return i.IsInt64() && i.Int64() == int64(n)
		}
		return jsonFloat(v) == float64(n)
	case bool:
		return (v && n == 1) || (!v && n == 0)
	}
	return false
}

func validateReleaseIdentity(manifest map[string]any) error {
	source, selection := object(manifest, "source"), object(manifest, "selection")
	integrity, provenance := object(manifest, "integrity"), object(manifest, "provenance")
	identity := manifestIdentity(manifest)
	if source["commit"] != pinnedCommit || source["tag"] != pinnedTag || provenance["kind"] != "release-pinned" {
		return errors.New("manifest source is not the pinned Effect 4.0.1 release")
	}
	if !isPyInt(selection["count"], releaseReferenceCount) || len(identity["licenses"].([]any)) != releaseLicenseCount {
		return errors.New("manifest release selection counts are invalid")
	}
	if integrity["referenceIdentitySha256"] != releaseReferenceIdentitySHA256 || integrity["licenseIdentitySha256"] != releaseLicenseIdentitySHA256 || integrity["rootSha256"] != releaseRootIdentitySHA256 {
		return errors.New("manifest release identity digest is not the independently pinned corpus")
	}
	return nil
}

// verifyManifest compares a committed manifest with the one recomputed from the pinned commit.
func verifyManifest(expected map[string]any, manifestPath, commit, tag string) error {
	text, err := readText(manifestPath)
	var actual any
	if err == nil {
		actual, err = decodeJSON(text)
	}
	if err != nil {
		return fmt.Errorf("manifest is not readable UTF-8 JSON: %s", manifestPath)
	}
	if difference, ok := firstDifference(actual, expected, ""); ok {
		return fmt.Errorf("manifest differs from the pinned checkout at %s", difference)
	}
	if text != renderManifest(expected) {
		return errors.New("manifest is not in canonical form; run with --refresh")
	}
	if commit == pinnedCommit && tag == pinnedTag {
		return validateReleaseIdentity(expected)
	}
	return nil
}

// verify checks the pinned checkout and committed manifest and returns the
// verified corpus for consumers.
func verify(root, commit, tag string) (pinnedCorpus, error) {
	corpus, err := checkedOut(root, commit, tag)
	if err != nil {
		return pinnedCorpus{}, err
	}
	if err := verifyManifest(corpus.manifest, filepath.Join(root, manifestRelative), commit, tag); err != nil {
		return pinnedCorpus{}, err
	}
	return corpus, nil
}

// refresh rewrites the committed manifest from the pinned checkout.
func refresh(root, commit, tag string) (map[string]any, error) {
	corpus, err := checkedOut(root, commit, tag)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(root, manifestRelative), []byte(renderManifest(corpus.manifest)), 0o644); err != nil {
		return nil, err
	}
	return corpus.manifest, nil
}
