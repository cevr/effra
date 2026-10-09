package main

// Capture and verify the pinned Foldkit workspace as reference-only source.
//
// The snapshot under conformance/upstream/foldkit-d21db423 preserves the
// tracked Foldkit workspace at one commit, read from its immutable git objects,
// minus the vendored repos/ reference trees and generated or installed
// directories. Effra owns its manifest: the selection rule, the Git identity
// and sha256 of every file, and integrity digests pinned independently below.
// conformance/framework-ports/manifest.json is the example/host coverage
// matrix over that snapshot. Neither is a claim that Effra executes or passes
// any Foldkit example.

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"
)

const (
	foldkitCommit                  = "d21db423ac0ba676b22aa7a7b4041ba409ec2c52"
	foldkitTree                    = "ccbd07912275e62385bd9fdccaf2e1e695cadb4b"
	foldkitRepository              = "foldkit/foldkit"
	foldkitRelease                 = "0.167.0"
	foldkitWorkspaceEffect         = "4.0.0"
	foldkitWorkspacePackageManager = "pnpm@11.8.0"
	foldkitSourceURL               = "https://github.com/" + foldkitRepository + "/tree/" + foldkitCommit
	foldkitDefaultSource           = "/home/exedev/.cache/repo/foldkit/foldkit"
	foldkitSnapshotRelative        = "conformance/upstream/foldkit-d21db423"
	foldkitCorpusRelative          = "conformance/framework-ports"
	foldkitCanonicalEncoding       = "json-sorted-compact-utf8-v1"

	// Set from the first verified capture. These constants make offline
	// validation reject edits that change both a captured byte and its
	// manifest hash.
	foldkitExpectedFileCount              = 2795
	foldkitExpectedExampleInventorySHA256 = "7cca6e854630d8ad539801f5914a995590358294f2ee64086b4376e0923f57c8"
	foldkitExpectedRootSHA256             = "69d0606c683d1175bf9c2f7975686113379ae11687cdb29ec9cc3c4cfaf55152"
	foldkitNoticeContent                  = "This snapshot preserves the tracked Foldkit workspace at the commit in " +
		"manifest.json, with the upstream LICENSE retained byte-for-byte. The " +
		"tracked examples, packages, internal sources, scripts, package-manager " +
		"files, and workspace configuration are included. The vendored repos/ " +
		"reference trees and generated or installed dependency/cache directories " +
		"are excluded; repository guidance states that repos/ is not imported by " +
		"package or example source. No dependencies were installed and no source " +
		"was executed as part of this capture. This is immutable comparison data, " +
		"not Effra coverage or a claim that the snapshot currently builds.\n"
)

var foldkitExampleIDs = []string{
	"api-cache",
	"api-cache-query",
	"auth",
	"canvas-art",
	"charting",
	"counter",
	"counters",
	"crash-view",
	"embedding",
	"form",
	"generative-art",
	"interrupting-commands",
	"job-application",
	"kanban",
	"managed-resource-layer",
	"map",
	"personal-blog",
	"pixel-art",
	"query-sync",
	"route-transitions",
	"routing",
	"shopping-cart",
	"slow-warnings",
	"snake",
	"ssg",
	"ssr",
	"state-machine",
	"stopwatch",
	"todo",
	"ui-showcase",
	"view-transitions",
	"weather",
	"web-components",
	"websocket-chat",
}

var foldkitHosts = []string{
	"foldkit-reference",
	"react-dom",
	"solid-2-dom",
	"solid-yield",
	"effra-go",
	"effra-effect-js",
}

var foldkitFocus = map[string]string{
	"api-cache":              "Shared API cache and tabbed data reads",
	"api-cache-query":        "Typed query cache with explicit data consumers",
	"auth":                   "Session restoration, routing, and login",
	"canvas-art":             "Canvas commands and generated drawing state",
	"charting":               "Chart and data-visualization interactions",
	"counter":                "Minimal model, messages, update, and view",
	"counters":               "Multiple independent counter models",
	"crash-view":             "Failure presentation at a view boundary",
	"embedding":              "Embedding an application through host ports",
	"form":                   "Field validation and asynchronous submission",
	"generative-art":         "Procedural artwork and generated commands",
	"interrupting-commands":  "Keyed cancellation of concurrent uploads",
	"job-application":        "Calendar and job-application form flow",
	"kanban":                 "Persisted board state and task movement",
	"managed-resource-layer": "Layer acquisition through managed resource lifetime",
	"map":                    "Map and geolocation host integration",
	"personal-blog":          "Routed article pages and content loading",
	"pixel-art":              "Pixel editing and persisted drawing state",
	"query-sync":             "URL query state synchronized with model state",
	"route-transitions":      "Route changes with explicit transition state",
	"routing":                "Routed application state and navigation commands",
	"shopping-cart":          "Cart state and derived checkout information",
	"slow-warnings":          "Slow work observation and warning UI",
	"snake":                  "Tick-driven interactive game state",
	"ssg":                    "Static-site generation over route data",
	"ssr":                    "Server rendering and initial application state",
	"state-machine":          "Guarded checkout state machine",
	"stopwatch":              "Time commands and subscriptions",
	"todo":                   "Persisted task editing",
	"ui-showcase":            "Reusable UI controls and routing",
	"view-transitions":       "Browser view-transition lifecycle",
	"weather":                "Typed HTTP requests and async result state",
	"web-components":         "Custom-element definition and host boundary",
	"websocket-chat":         "Socket resource and streaming messages",
}

var foldkitExcludedPrefixes = []string{"repos/"}

var foldkitExcludedDirectoryNames = []string{
	".git",
	"node_modules",
	".cache",
	".vite",
	".turbo",
	"coverage",
	".next",
	".output",
	".vercel",
	".nyc_output",
}

var foldkitExcludedFileSuffixes = []string{".tsbuildinfo"}

var foldkitTrackedModes = map[string]bool{"100644": true, "100755": true, "120000": true}

// foldkitPin is the upstream identity a capture selects and records. The CLI
// always uses pinnedFoldkit; tests capture a fixture repository under its own pin.
type foldkitPin struct {
	commit, tree, sourceURL string
	exampleIDs              []string
	focus                   map[string]string
}

var pinnedFoldkit = foldkitPin{
	commit:     foldkitCommit,
	tree:       foldkitTree,
	sourceURL:  foldkitSourceURL,
	exampleIDs: foldkitExampleIDs,
	focus:      foldkitFocus,
}

// foldkitEntry is one tracked blob of the pinned commit.
type foldkitEntry struct {
	path, mode, objectType, oid string
	size                        int
}

// purePath is str(pathlib.PurePosixPath(value)): repeated separators and "."
// segments collapse, ".." stays, and exactly two leading slashes survive.
func purePath(value string) string {
	anchor := pathAnchor(value)
	joined := anchor + strings.Join(pathParts(value), "/")
	if joined == "" {
		return "."
	}
	return joined
}

func pathAnchor(value string) string {
	switch {
	case strings.HasPrefix(value, "//") && !strings.HasPrefix(value, "///"):
		return "//"
	case strings.HasPrefix(value, "/"):
		return "/"
	}
	return ""
}

// pureJoin is pathlib's base / relative / ... for relative components.
func pureJoin(base string, relative ...string) string {
	return purePath(strings.Join(append([]string{base}, relative...), "/"))
}

// pureParent is pathlib's lexical Path.parent.
func pureParent(value string) string {
	value = purePath(value)
	index := strings.LastIndex(value, "/")
	switch {
	case index < 0:
		return "."
	case index < len(pathAnchor(value)) || value[:index] == "":
		return value[:index+1]
	}
	return value[:index]
}

// pureRelativeTo is pathlib's PurePath.relative_to(base).as_posix().
func pureRelativeTo(value, base string) (string, bool) {
	value, base = purePath(value), purePath(base)
	if pathAnchor(value) != pathAnchor(base) {
		return "", false
	}
	valueParts, baseParts := pathParts(value), pathParts(base)
	if len(baseParts) > len(valueParts) || !slices.Equal(valueParts[:len(baseParts)], baseParts) {
		return "", false
	}
	if relative := strings.Join(valueParts[len(baseParts):], "/"); relative != "" {
		return relative, true
	}
	return ".", true
}

// pySubscript is Python's value[key] for a str key on a json.loads value,
// with the KeyError or TypeError text the original surfaced.
func pySubscript(value any, key string) (any, error) {
	switch v := value.(type) {
	case map[string]any:
		item, ok := v[key]
		if !ok {
			return nil, errors.New(pyStringRepr(key))
		}
		return item, nil
	case []any:
		return nil, errors.New("list indices must be integers or slices, not str")
	case string:
		return nil, errors.New("string indices must be integers, not 'str'")
	}
	return nil, fmt.Errorf("'%s' object is not subscriptable", pyType(value))
}

// pyGet is Python's value.get(key) on a json.loads value: None when missing,
// and the AttributeError text when value is not a dict.
func pyGet(value any, key string) (any, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("'%s' object has no attribute 'get'", pyType(value))
	}
	return object[key], nil
}

// pyInt is Python's int() on a decimal size field.
func pyInt(text string) (int, error) {
	value, err := strconv.Atoi(text)
	if err != nil {
		return 0, fmt.Errorf("invalid literal for int() with base 10: %s", pyStringRepr(text))
	}
	return value, nil
}

func asciiFields(raw []byte) ([]string, error) {
	for index, b := range raw {
		if b >= 0x80 {
			return nil, fmt.Errorf("'ascii' codec can't decode byte 0x%02x in position %d: ordinal not in range(128)", b, index)
		}
	}
	return strings.FieldsFunc(string(raw), pyIsSpace), nil
}

func unpackCount(fields []string, want int) error {
	if len(fields) < want {
		return fmt.Errorf("not enough values to unpack (expected %d, got %d)", want, len(fields))
	}
	if len(fields) > want {
		return fmt.Errorf("too many values to unpack (expected %d)", want)
	}
	return nil
}

func foldkitGit(source string, args ...string) ([]byte, error) {
	result, err := gitProcess(source, nil, args...)
	if err != nil {
		return nil, err
	}
	if result.code != 0 {
		return nil, fmt.Errorf("git %s failed: %s", strings.Join(args, " "), pyStrip(strings.ToValidUTF8(string(result.stderr), "�")))
	}
	return result.stdout, nil
}

// foldkitSafePath accepts a nonempty relative path with no parent segment.
// Like the pathlib original, "." segments collapse and are accepted.
func foldkitSafePath(value string) error {
	if value == "" || strings.ContainsAny(value, "\\\x00") || strings.HasPrefix(value, "/") || slices.Contains(pathParts(value), "..") {
		return fmt.Errorf("unsafe snapshot path: %s", pyStringRepr(value))
	}
	return nil
}

func foldkitExcludedReason(relative string) string {
	normalized := strings.ReplaceAll(relative, "\\", "/")
	for _, prefix := range foldkitExcludedPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return "vendored-reference-tree"
		}
	}
	parts := pathParts(normalized)
	for _, part := range parts {
		if slices.Contains(foldkitExcludedDirectoryNames, part) {
			return "installed-or-generated-directory"
		}
	}
	for _, part := range parts {
		for _, suffix := range foldkitExcludedFileSuffixes {
			if strings.HasSuffix(part, suffix) {
				return "generated-typescript-build-info"
			}
		}
	}
	return ""
}

// foldkitTreeEntries lists the commit's included blobs in UTF-8 byte order and
// counts the excluded ones by reason.
func foldkitTreeEntries(source, commit string) ([]foldkitEntry, map[string]map[string]int, error) {
	raw, err := foldkitGit(source, "ls-tree", "-r", "-l", "-z", "--full-tree", commit)
	if err != nil {
		return nil, nil, err
	}
	var included []foldkitEntry
	excluded := map[string]map[string]int{}
	for _, record := range bytes.Split(raw, []byte{0}) {
		if len(record) == 0 {
			continue
		}
		header, rawPath, ok := bytes.Cut(record, []byte("\t"))
		if !ok {
			return nil, nil, errors.New("not enough values to unpack (expected 2, got 1)")
		}
		fields, err := asciiFields(header)
		if err != nil {
			return nil, nil, err
		}
		if err := unpackCount(fields, 4); err != nil {
			return nil, nil, err
		}
		mode, objectType, oid, rawSize := fields[0], fields[1], fields[2], fields[3]
		if !utf8.Valid(rawPath) {
			return nil, nil, fmt.Errorf("'utf-8' codec can't decode tracked path %s", strconv.Quote(string(rawPath)))
		}
		relative := string(rawPath)
		if err := foldkitSafePath(relative); err != nil {
			return nil, nil, err
		}
		size, err := pyInt(rawSize)
		if err != nil {
			return nil, nil, err
		}
		if reason := foldkitExcludedReason(relative); reason != "" {
			current := excluded[reason]
			if current == nil {
				current = map[string]int{"files": 0, "bytes": 0}
				excluded[reason] = current
			}
			current["files"]++
			current["bytes"] += size
			continue
		}
		if objectType != "blob" || !foldkitTrackedModes[mode] {
			return nil, nil, fmt.Errorf("unsupported tracked entry: %s %s %s", mode, objectType, relative)
		}
		included = append(included, foldkitEntry{relative, mode, objectType, oid, size})
	}
	sort.SliceStable(included, func(i, j int) bool { return included[i].path < included[j].path })
	return included, excluded, nil
}

func foldkitExampleInventory(entries []foldkitEntry) []string {
	seen := map[string]bool{}
	for _, entry := range entries {
		if parts := pathParts(entry.path); len(parts) >= 3 && parts[0] == "examples" {
			seen[parts[1]] = true
		}
	}
	result := make([]string, 0, len(seen))
	for example := range seen {
		result = append(result, example)
	}
	sort.Strings(result)
	return result
}

func foldkitRequireInventory(entries []foldkitEntry, exampleIDs []string) ([]string, error) {
	actual := foldkitExampleInventory(entries)
	expected := slices.Sorted(slices.Values(exampleIDs))
	if !slices.Equal(actual, expected) {
		var missing, unexpected []string
		for _, example := range expected {
			if !slices.Contains(actual, example) {
				missing = append(missing, example)
			}
		}
		for _, example := range actual {
			if !slices.Contains(expected, example) {
				unexpected = append(unexpected, example)
			}
		}
		return nil, fmt.Errorf("pinned example inventory mismatch; missing=%s, unexpected=%s", pyRepr(stringList(missing)), pyRepr(stringList(unexpected)))
	}
	return actual, nil
}

func foldkitValidatePin(source string, pin foldkitPin) error {
	resolved, err := foldkitGit(source, "rev-parse", pin.commit+"^{commit}")
	if err != nil {
		return err
	}
	tree, err := foldkitGit(source, "rev-parse", pin.commit+"^{tree}")
	if err != nil {
		return err
	}
	if got := pyStrip(string(resolved)); got != pin.commit {
		return fmt.Errorf("source commit mismatch: expected %s, got %s", pin.commit, got)
	}
	if got := pyStrip(string(tree)); got != pin.tree {
		return fmt.Errorf("source tree mismatch: expected %s, got %s", pin.tree, got)
	}
	return nil
}

// foldkitReadBlobData reads every entry's bytes from git objects with one
// cat-file process, refusing any header or body that does not match.
func foldkitReadBlobData(source string, entries []foldkitEntry) (map[string][]byte, error) {
	if len(entries) == 0 {
		return nil, errors.New("source selection is empty")
	}
	var request bytes.Buffer
	for _, entry := range entries {
		request.WriteString(entry.oid + "\n")
	}
	process, err := gitProcess(source, request.Bytes(), "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	if process.code != 0 {
		return nil, errors.New("git cat-file --batch failed: " + pyStrip(strings.ToValidUTF8(string(process.stderr), "�")))
	}
	output := process.stdout
	offset := 0
	result := map[string][]byte{}
	for _, entry := range entries {
		end := bytes.IndexByte(output[offset:], '\n')
		if end < 0 {
			return nil, fmt.Errorf("truncated cat-file header for %s", entry.path)
		}
		end += offset
		header, err := asciiFields(output[offset:end])
		if err != nil {
			return nil, err
		}
		if len(header) != 3 {
			return nil, fmt.Errorf("invalid cat-file header for %s", entry.path)
		}
		size, err := pyInt(header[2])
		if err != nil {
			return nil, err
		}
		if header[0] != entry.oid || header[1] != "blob" || size != entry.size {
			return nil, fmt.Errorf("cat-file identity mismatch for %s", entry.path)
		}
		start := end + 1
		bodyEnd := start + size
		if bodyEnd < start || bodyEnd >= len(output) || output[bodyEnd] != '\n' {
			return nil, fmt.Errorf("truncated cat-file body for %s", entry.path)
		}
		result[entry.path] = output[start:bodyEnd]
		offset = bodyEnd + 1
	}
	if offset != len(output) {
		return nil, errors.New("unexpected trailing cat-file output")
	}
	return result, nil
}

// loadsBytes is json.loads on UTF-8 bytes, which drops a leading byte order mark.
func loadsBytes(data []byte) (any, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if !utf8.Valid(data) {
		return nil, errNotText
	}
	return decodeJSON(string(data))
}

// foldkitSourceVersions reads the workspace package versions the pin records.
func foldkitSourceVersions(data map[string][]byte) (map[string]string, error) {
	unreadable := func(err error) error {
		return fmt.Errorf("could not read pinned workspace package versions: %v", err)
	}
	documents := make([]any, 0, 2)
	for _, name := range []string{"package.json", "packages/foldkit/package.json"} {
		body, ok := data[name]
		if !ok {
			return nil, unreadable(errors.New(pyStringRepr(name)))
		}
		value, err := loadsBytes(body)
		if errors.Is(err, errNotText) {
			// UnicodeDecodeError is not among the refusals the original wraps.
			return nil, fmt.Errorf("'utf-8' codec can't decode %s", name)
		}
		if err != nil {
			return nil, unreadable(err)
		}
		documents = append(documents, value)
	}
	root, foldkit := documents[0], documents[1]
	packageManager, err := pyGet(root, "packageManager")
	if err != nil {
		return nil, err
	}
	var devDependencies any = map[string]any{}
	if object, ok := root.(map[string]any); ok {
		if value, present := object["devDependencies"]; present {
			devDependencies = value
		}
	}
	effect, err := pyGet(devDependencies, "effect")
	if err != nil {
		return nil, err
	}
	version, err := pyGet(foldkit, "version")
	if err != nil {
		return nil, err
	}
	keys := []string{"workspacePackageManager", "effect", "foldkit"}
	actual := []any{packageManager, effect, version}
	expected := []string{foldkitWorkspacePackageManager, foldkitWorkspaceEffect, foldkitRelease}
	result := map[string]string{}
	for index, key := range keys {
		text, ok := actual[index].(string)
		if !ok || text != expected[index] {
			parts := make([]string, len(keys))
			for i, name := range keys {
				parts[i] = pyStringRepr(name) + ": " + pyRepr(actual[i])
			}
			return nil, fmt.Errorf("pinned workspace versions mismatch: {%s}", strings.Join(parts, ", "))
		}
		result[key] = text
	}
	return result, nil
}

// foldkitFileIdentity projects manifest file entries onto their identity fields.
func foldkitFileIdentity(files []any) ([]any, error) {
	result := make([]any, 0, len(files))
	for _, item := range files {
		identity := map[string]any{}
		for _, key := range []string{"path", "mode", "gitBlob", "bytes", "sha256"} {
			value, err := pySubscript(item, key)
			if err != nil {
				return nil, err
			}
			identity[key] = value
		}
		result = append(result, identity)
	}
	return result, nil
}

func foldkitManifestFor(entries []foldkitEntry, data map[string][]byte, inventory []string, excluded map[string]map[string]int, pin foldkitPin) (map[string]any, error) {
	versions, err := foldkitSourceVersions(data)
	if err != nil {
		return nil, err
	}
	files := make([]any, 0, len(entries))
	for _, entry := range entries {
		files = append(files, map[string]any{
			"path":    entry.path,
			"mode":    entry.mode,
			"gitBlob": entry.oid,
			"bytes":   number(len(data[entry.path])),
			"sha256":  sha256Hex(data[entry.path]),
		})
	}
	fileIdentity, err := foldkitFileIdentity(files)
	if err != nil {
		return nil, err
	}
	source := map[string]any{
		"repository": foldkitRepository,
		"commit":     pin.commit,
		"tree":       pin.tree,
		"url":        pin.sourceURL,
	}
	for key, value := range versions {
		source[key] = value
	}
	excludedCounts := map[string]any{}
	for reason, counts := range excluded {
		excludedCounts[reason] = map[string]any{"files": number(counts["files"]), "bytes": number(counts["bytes"])}
	}
	identity := map[string]any{
		"schemaVersion": number(1),
		"status":        "source-reference-only-not-executed",
		"source":        source,
		"selection": map[string]any{
			"kind":                   "all-tracked-workspace-files-except-vendored-reference-and-generated-paths",
			"exampleDirectories":     stringList(inventory),
			"exampleDirectoryCount":  number(len(inventory)),
			"includedFileCount":      number(len(files)),
			"excludedPrefixes":       stringList(foldkitExcludedPrefixes),
			"excludedDirectoryNames": stringList(slices.Sorted(slices.Values(foldkitExcludedDirectoryNames))),
			"excludedFileSuffixes":   stringList(foldkitExcludedFileSuffixes),
			"excludedCounts":         excludedCounts,
		},
		"files": fileIdentity,
	}
	manifest := map[string]any{}
	for key, value := range identity {
		manifest[key] = value
	}
	manifest["integrity"] = map[string]any{
		"algorithm":              "sha256",
		"canonicalEncoding":      foldkitCanonicalEncoding,
		"exampleInventorySha256": canonicalSHA256(stringList(inventory)),
		"fileIdentitySha256":     canonicalSHA256(fileIdentity),
		"rootSha256":             canonicalSHA256(identity),
	}
	return manifest, nil
}

// foldkitSymlinkReferent is posixpath.normpath(posixpath.join(posixpath.dirname(relative), target)).
func foldkitSymlinkReferent(relative, target string) string {
	return path.Clean(path.Join(path.Dir(relative), target))
}

func foldkitSafeSymlinkTarget(relative string, target []byte) (string, error) {
	if !utf8.Valid(target) {
		return "", fmt.Errorf("symlink target is not UTF-8: %s", relative)
	}
	value := string(target)
	if value == "" || strings.ContainsAny(value, "\\\x00") || strings.HasPrefix(value, "/") {
		return "", fmt.Errorf("unsafe symlink target in source: %s -> %s", relative, pyStringRepr(value))
	}
	if resolved := foldkitSymlinkReferent(relative, value); resolved == ".." || strings.HasPrefix(resolved, "../") {
		return "", fmt.Errorf("symlink escapes snapshot: %s -> %s", relative, pyStringRepr(value))
	}
	return value, nil
}

func foldkitWriteTree(stage string, entries []foldkitEntry, data map[string][]byte) error {
	for _, entry := range entries {
		if err := foldkitSafePath(entry.path); err != nil {
			return err
		}
		destination := pureJoin(stage, entry.path)
		if err := os.MkdirAll(pureParent(destination), 0o777); err != nil {
			return err
		}
		body := data[entry.path]
		if entry.mode == "120000" {
			target, err := foldkitSafeSymlinkTarget(entry.path, body)
			if err != nil {
				return err
			}
			if err := os.Symlink(target, destination); err != nil {
				return err
			}
			continue
		}
		if err := os.WriteFile(destination, body, 0o666); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if entry.mode == "100755" {
			mode = 0o755
		}
		if err := os.Chmod(destination, mode); err != nil {
			return err
		}
	}
	return nil
}

// writeFoldkitJSON writes json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2) and a newline.
func writeFoldkitJSON(destination string, value any) error {
	return os.WriteFile(destination, []byte(prettyJSON(value)+"\n"), 0o666)
}

func foldkitCorpusManifestFor(snapshot, repositoryRoot string, pin foldkitPin) (map[string]any, error) {
	reference, ok := pureRelativeTo(snapshot, repositoryRoot)
	if !ok {
		return nil, fmt.Errorf("corpus snapshot is outside repository root: %s", purePath(snapshot))
	}
	if reference == "" || reference == "." {
		return nil, errors.New("corpus snapshot path is empty")
	}
	focus := func(example string) (string, error) {
		value, ok := pin.focus[example]
		if !ok {
			return "", errors.New(pyStringRepr(example))
		}
		return value, nil
	}
	coverage := []any{}
	examples := []any{}
	for _, example := range pin.exampleIDs {
		exampleFocus, err := focus(example)
		if err != nil {
			return nil, err
		}
		evidence := reference + "/examples/" + example + "/package.json"
		for _, host := range foldkitHosts {
			status, rowEvidence := "pending", []any{}
			note := "No executable port or Effra fixture is present in unit 1."
			if host == "foldkit-reference" {
				status, rowEvidence = "reference-only", []any{evidence}
				note = "Pinned upstream source only; not Effra execution coverage."
			}
			coverage = append(coverage, map[string]any{
				"example":  example,
				"focus":    exampleFocus,
				"host":     host,
				"status":   status,
				"evidence": rowEvidence,
				"note":     note,
			})
		}
		examples = append(examples, map[string]any{"id": example, "focus": exampleFocus})
	}
	return map[string]any{
		"schemaVersion": number(1),
		"status":        "unit-1-reference-capture; host-ports-pending",
		"source": map[string]any{
			"repository":             foldkitRepository,
			"commit":                 pin.commit,
			"tree":                   pin.tree,
			"url":                    pin.sourceURL,
			"foldkitVersion":         foldkitRelease,
			"workspaceEffectVersion": foldkitWorkspaceEffect,
		},
		"snapshot": map[string]any{
			"path":     reference,
			"manifest": reference + "/manifest.json",
			"status":   "reference-only-not-executed",
		},
		"hosts":    stringList(foldkitHosts),
		"examples": examples,
		"coverage": coverage,
	}, nil
}

// pyIsFile is pathlib's Path.is_file: missing paths read as false, other
// stat failures propagate.
func pyIsFile(name string) (bool, error) {
	info, err := os.Stat(name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) || errors.Is(err, syscall.ELOOP) || errors.Is(err, syscall.EBADF) {
			return false, nil
		}
		return false, err
	}
	return info.Mode().IsRegular(), nil
}

// readFoldkitJSON is json.loads(path.read_text(encoding="utf-8")).
func readFoldkitJSON(name string) (any, error) {
	text, err := readText(name)
	if err != nil {
		return nil, err
	}
	return decodeJSON(text)
}

func validateFoldkitCorpusManifest(manifestPath, snapshot, repositoryRoot string) error {
	decoded, err := readFoldkitJSON(manifestPath)
	if err != nil {
		return fmt.Errorf("invalid framework corpus manifest: %v", err)
	}
	manifest, ok := decoded.(map[string]any)
	if !ok {
		return errors.New("invalid framework corpus manifest: root must be an object")
	}
	expected, err := foldkitCorpusManifestFor(snapshot, repositoryRoot, pinnedFoldkit)
	if err != nil {
		return err
	}
	for _, key := range []string{"schemaVersion", "status", "source", "snapshot", "hosts", "examples"} {
		if !pyEqual(manifest[key], expected[key]) {
			return fmt.Errorf("framework corpus manifest has an unexpected %s field", key)
		}
	}
	rows, ok := manifest["coverage"].([]any)
	if !ok {
		return errors.New("framework corpus manifest coverage must be a list")
	}
	if len(rows) != len(foldkitExampleIDs)*len(foldkitHosts) {
		return errors.New("framework corpus manifest has an incomplete example/host matrix")
	}
	expectedRows := expected["coverage"].([]any)
	required := []string{"evidence", "example", "focus", "host", "note", "status"}
	for index, value := range rows {
		row, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("framework corpus coverage row %d must be an object", index)
		}
		var missing, unexpected []string
		for _, key := range required {
			if _, present := row[key]; !present {
				missing = append(missing, key)
			}
		}
		for _, key := range sortedKeys(row) {
			if !slices.Contains(required, key) {
				unexpected = append(unexpected, key)
			}
		}
		if len(missing) > 0 || len(unexpected) > 0 {
			var detail []string
			if len(missing) > 0 {
				detail = append(detail, "missing "+strings.Join(missing, ", "))
			}
			if len(unexpected) > 0 {
				detail = append(detail, "unexpected "+strings.Join(unexpected, ", "))
			}
			return fmt.Errorf("framework corpus coverage row %d has malformed fields (%s)", index, strings.Join(detail, "; "))
		}
		expectedRow := expectedRows[index].(map[string]any)
		for _, key := range []string{"example", "focus", "host"} {
			if !pyEqual(row[key], expectedRow[key]) {
				return fmt.Errorf("framework corpus coverage row %d has unexpected %s: %s", index, key, pyRepr(row[key]))
			}
		}
	}
	// Every example and host now equals its expected string.
	pairs := map[[2]string]bool{}
	for _, value := range rows {
		row := value.(map[string]any)
		pairs[[2]string{row["example"].(string), row["host"].(string)}] = true
	}
	if len(pairs) != len(rows) {
		return errors.New("framework corpus manifest contains duplicate example/host rows")
	}
	snapshotReference := expected["snapshot"].(map[string]any)["path"].(string)
	for index, value := range rows {
		row, expectedRow := value.(map[string]any), expectedRows[index].(map[string]any)
		label := row["example"].(string) + ":" + row["host"].(string)
		status := row["status"]
		switch status.(type) {
		case []any:
			return errors.New("unhashable type: 'list'")
		case map[string]any:
			return errors.New("unhashable type: 'dict'")
		}
		statusText, _ := status.(string)
		if statusText != "reference-only" && statusText != "pending" && statusText != "implemented-and-verified" {
			return fmt.Errorf("invalid corpus coverage status at row %d (%s): %s", index, label, pyRepr(status))
		}
		evidenceList, ok := row["evidence"].([]any)
		evidence := make([]string, 0, len(evidenceList))
		for _, item := range evidenceList {
			text, isText := item.(string)
			ok = ok && isText
			evidence = append(evidence, text)
		}
		if !ok {
			return fmt.Errorf("invalid evidence list at row %d (%s)", index, label)
		}
		if note, isText := row["note"].(string); !isText || pyStrip(note) == "" {
			return fmt.Errorf("missing coverage note at row %d (%s)", index, label)
		}
		switch {
		case row["host"] == "foldkit-reference":
			if statusText != "reference-only" || !pyEqual(evidenceList, expectedRow["evidence"]) {
				return fmt.Errorf("Foldkit source row %d (%s) must remain reference-only with its source evidence", index, label)
			}
		case statusText == "pending" && len(evidence) > 0:
			return fmt.Errorf("pending host row has evidence at row %d: %s", index, label)
		case statusText == "reference-only" && len(evidence) == 0:
			return fmt.Errorf("reference-only host row has no source evidence at row %d: %s", index, label)
		case statusText == "implemented-and-verified" && len(evidence) == 0:
			return fmt.Errorf("implemented host row has no verification evidence at row %d: %s", index, label)
		case statusText == "implemented-and-verified":
			for _, item := range evidence {
				if item == snapshotReference || strings.HasPrefix(item, snapshotReference+"/") {
					return fmt.Errorf("implemented host row has pinned upstream source-reference evidence at row %d: %s", index, label)
				}
			}
			return fmt.Errorf("implemented host row is not accepted before an executable runner verifies an authored target at row %d: %s", index, label)
		}
		for _, item := range evidence {
			if err := foldkitSafePath(item); err != nil {
				return err
			}
			present, err := pyIsFile(pureJoin(repositoryRoot, item))
			if err != nil {
				return err
			}
			if !present {
				return fmt.Errorf("missing corpus evidence file: %s", item)
			}
		}
	}
	return nil
}

// foldkitSnapshotFilePaths walks like os.walk(root, followlinks=False): it
// refuses directory symlinks and special files and, like os.walk, skips a
// directory it cannot list.
func foldkitSnapshotFilePaths(root string) (map[string]bool, error) {
	result := map[string]bool{}
	var walk func(current string) error
	walk = func(current string) error {
		entries, err := os.ReadDir(current)
		if err != nil {
			return nil
		}
		var directories, files []string
		for _, entry := range entries {
			// os.walk classifies with DirEntry.is_dir(), which follows symlinks.
			isDirectory := entry.IsDir()
			if entry.Type()&fs.ModeSymlink != 0 {
				if info, err := os.Stat(current + "/" + entry.Name()); err == nil {
					isDirectory = info.IsDir()
				}
			}
			if isDirectory {
				directories = append(directories, entry.Name())
			} else {
				files = append(files, entry.Name())
			}
		}
		for _, name := range directories {
			candidate := current + "/" + name
			info, err := os.Lstat(candidate)
			if err != nil {
				return err
			}
			if info.Mode()&fs.ModeSymlink != 0 {
				return fmt.Errorf("directory symlink is not allowed in snapshot: %s", candidate)
			}
			if !info.IsDir() {
				return fmt.Errorf("unexpected directory entry: %s", candidate)
			}
		}
		for _, name := range files {
			candidate := current + "/" + name
			relative, _ := pureRelativeTo(candidate, root)
			info, err := os.Lstat(candidate)
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() && info.Mode()&fs.ModeSymlink == 0 {
				return fmt.Errorf("unsupported snapshot file type: %s", relative)
			}
			result[relative] = true
		}
		for _, name := range directories {
			if err := walk(current + "/" + name); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(purePath(root)); err != nil {
		return nil, err
	}
	return result, nil
}

func gitBlobOID(data []byte) string {
	hash := sha1.New()
	fmt.Fprintf(hash, "blob %d\x00", len(data))
	hash.Write(data)
	return hex.EncodeToString(hash.Sum(nil))
}

func firstSorted(values map[string]bool, limit int) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > limit {
		keys = keys[:limit]
	}
	return pyRepr(stringList(keys))
}

// validateFoldkitSnapshot verifies the committed snapshot against its
// manifest and the independently pinned identity, and returns the manifest.
func validateFoldkitSnapshot(snapshot string) (map[string]any, error) {
	snapshot = purePath(snapshot)
	if info, err := os.Lstat(snapshot); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("missing or unsafe source snapshot: %s", snapshot)
	}
	decoded, err := readFoldkitJSON(snapshot + "/manifest.json")
	if err != nil {
		return nil, fmt.Errorf("invalid Foldkit snapshot manifest: %v", err)
	}
	manifest, ok := decoded.(map[string]any)
	if !ok {
		return nil, errors.New("invalid Foldkit snapshot manifest: root must be an object")
	}
	if !isPyInt(manifest["schemaVersion"], 1) || manifest["status"] != "source-reference-only-not-executed" {
		return nil, errors.New("unsupported Foldkit snapshot schema or status")
	}
	source, ok := manifest["source"].(map[string]any)
	if !ok || source["commit"] != foldkitCommit || source["tree"] != foldkitTree {
		return nil, errors.New("Foldkit snapshot does not identify the pinned commit and tree")
	}
	if source["repository"] != foldkitRepository || source["url"] != foldkitSourceURL {
		return nil, errors.New("Foldkit snapshot repository provenance mismatch")
	}
	files, ok := manifest["files"].([]any)
	if !ok {
		return nil, errors.New("Foldkit snapshot files must be a list")
	}
	expectedPaths := map[string]bool{}
	type referent struct{ path, referent string }
	var symlinkReferents []referent
	var verifiedPaths []string
	for _, value := range files {
		item, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("Foldkit snapshot file entry must be an object")
		}
		relative, ok := item["path"].(string)
		if !ok {
			return nil, errors.New("Foldkit snapshot file path must be a string")
		}
		if err := foldkitSafePath(relative); err != nil {
			return nil, err
		}
		if expectedPaths[relative] || foldkitExcludedReason(relative) != "" {
			return nil, fmt.Errorf("duplicate or excluded path in snapshot manifest: %s", relative)
		}
		expectedPaths[relative] = true
		candidate := pureJoin(snapshot, relative)
		info, err := os.Lstat(candidate)
		if err != nil {
			return nil, fmt.Errorf("missing snapshot file: %s", relative)
		}
		var body []byte
		switch mode := item["mode"]; {
		case mode == "120000":
			if info.Mode()&fs.ModeSymlink == 0 {
				return nil, fmt.Errorf("snapshot symlink mode mismatch: %s", relative)
			}
			link, err := os.Readlink(candidate)
			if err != nil {
				return nil, err
			}
			body = []byte(link)
			target, err := foldkitSafeSymlinkTarget(relative, body)
			if err != nil {
				return nil, err
			}
			symlinkReferents = append(symlinkReferents, referent{relative, foldkitSymlinkReferent(relative, target)})
		case mode == "100644" || mode == "100755":
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("snapshot regular-file mode mismatch: %s", relative)
			}
			if body, err = os.ReadFile(candidate); err != nil {
				return nil, err
			}
			// Git records only the owner executable bit (100644 or 100755); the
			// remaining permission bits follow the checkout umask.
			executable := info.Mode().Perm()&0o100 != 0
			if executable != (mode == "100755") {
				return nil, fmt.Errorf("snapshot executable mode mismatch: %s", relative)
			}
		default:
			return nil, fmt.Errorf("unsupported mode in snapshot manifest: %s", relative)
		}
		if !isPyInt(item["bytes"], len(body)) || item["sha256"] != sha256Hex(body) {
			return nil, fmt.Errorf("snapshot bytes or SHA-256 mismatch: %s", relative)
		}
		if item["gitBlob"] != gitBlobOID(body) {
			return nil, fmt.Errorf("snapshot Git blob identity mismatch: %s", relative)
		}
		verifiedPaths = append(verifiedPaths, relative)
	}
	for _, link := range symlinkReferents {
		if !expectedPaths[link.referent] {
			return nil, fmt.Errorf("snapshot symlink referent is absent from the manifest: %s -> %s", link.path, link.referent)
		}
	}
	// Paths are unique, so the entries equal their sorted order only when
	// each path's UTF-8 bytes follow the previous one's.
	for index := 1; index < len(verifiedPaths); index++ {
		if verifiedPaths[index-1] > verifiedPaths[index] {
			return nil, errors.New("Foldkit snapshot file entries are not in canonical order")
		}
	}
	actualPaths, err := foldkitSnapshotFilePaths(snapshot)
	if err != nil {
		return nil, err
	}
	delete(actualPaths, "manifest.json")
	delete(actualPaths, "NOTICE")
	if missing, extra := setDifference(expectedPaths, actualPaths), setDifference(actualPaths, expectedPaths); len(missing) > 0 || len(extra) > 0 {
		return nil, fmt.Errorf("snapshot file set mismatch; missing=%s, extra=%s", firstSorted(missing, 4), firstSorted(extra, 4))
	}
	notice, err := os.ReadFile(snapshot + "/NOTICE")
	if err != nil {
		return nil, err
	}
	if string(notice) != foldkitNoticeContent {
		return nil, errors.New("Foldkit NOTICE differs from the declared source-capture notice")
	}
	if len(files) != foldkitExpectedFileCount {
		return nil, fmt.Errorf("Foldkit snapshot has %d files, expected pinned count %d", len(files), foldkitExpectedFileCount)
	}
	selection, err := pySubscript(manifest, "selection")
	if err != nil {
		return nil, err
	}
	fileIdentity, err := foldkitFileIdentity(files)
	if err != nil {
		return nil, err
	}
	identity := map[string]any{
		"schemaVersion": manifest["schemaVersion"],
		"status":        manifest["status"],
		"source":        manifest["source"],
		"selection":     selection,
		"files":         fileIdentity,
	}
	integrity := manifest["integrity"]
	if canonicalSHA256(stringList(foldkitExampleIDs)) != foldkitExpectedExampleInventorySHA256 {
		return nil, errors.New("built-in Foldkit inventory fingerprint is inconsistent")
	}
	exampleDirectories, err := pySubscript(selection, "exampleDirectories")
	if err != nil {
		return nil, err
	}
	expectedIntegrity := map[string]any{
		"algorithm":              "sha256",
		"canonicalEncoding":      foldkitCanonicalEncoding,
		"exampleInventorySha256": canonicalSHA256(exampleDirectories),
		"fileIdentitySha256":     canonicalSHA256(fileIdentity),
		"rootSha256":             canonicalSHA256(identity),
	}
	if !pyEqual(integrity, expectedIntegrity) {
		return nil, errors.New("Foldkit snapshot integrity metadata mismatch")
	}
	if expectedIntegrity["exampleInventorySha256"] != foldkitExpectedExampleInventorySHA256 {
		return nil, errors.New("Foldkit example inventory digest differs from the pinned inventory")
	}
	if expectedIntegrity["rootSha256"] != foldkitExpectedRootSHA256 {
		return nil, errors.New("Foldkit file identity differs from the independently pinned snapshot")
	}
	// tuple(exampleDirectories): the inventory digest above already pins it to
	// the list of example ids, so only a list can still compare equal.
	switch exampleDirectories.(type) {
	case []any, string, map[string]any:
	default:
		return nil, fmt.Errorf("'%s' object is not iterable", pyType(exampleDirectories))
	}
	if !pyEqual(exampleDirectories, stringList(slices.Sorted(slices.Values(foldkitExampleIDs)))) {
		return nil, errors.New("Foldkit snapshot example directories are incomplete")
	}
	count, err := pySubscript(selection, "exampleDirectoryCount")
	if err != nil {
		return nil, err
	}
	if !isPyInt(count, 34) {
		return nil, errors.New("Foldkit snapshot example directories are incomplete")
	}
	workspace := map[string][]byte{}
	for _, value := range files {
		relative := value.(map[string]any)["path"].(string)
		if relative == "package.json" || relative == "packages/foldkit/package.json" {
			body, err := os.ReadFile(pureJoin(snapshot, relative))
			if err != nil {
				return nil, err
			}
			workspace[relative] = body
		}
	}
	if _, err := foldkitSourceVersions(workspace); err != nil {
		return nil, err
	}
	return manifest, nil
}

func setDifference(left, right map[string]bool) map[string]bool {
	result := map[string]bool{}
	for key := range left {
		if !right[key] {
			result[key] = true
		}
	}
	return result
}

// existsOrSymlink is Path.exists() or Path.is_symlink().
func existsOrSymlink(name string) (bool, error) {
	if _, err := os.Lstat(name); err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// foldkitSource is a pinned commit's selected blobs and the manifest they produce.
type foldkitSource struct {
	entries   []foldkitEntry
	inventory []string
	data      map[string][]byte
	manifest  map[string]any
}

// readFoldkitSource validates the pin and reads the selected blobs from git objects.
func readFoldkitSource(source string, pin foldkitPin) (foldkitSource, error) {
	if err := foldkitValidatePin(source, pin); err != nil {
		return foldkitSource{}, err
	}
	entries, excluded, err := foldkitTreeEntries(source, pin.commit)
	if err != nil {
		return foldkitSource{}, err
	}
	inventory, err := foldkitRequireInventory(entries, pin.exampleIDs)
	if err != nil {
		return foldkitSource{}, err
	}
	data, err := foldkitReadBlobData(source, entries)
	if err != nil {
		return foldkitSource{}, err
	}
	manifest, err := foldkitManifestFor(entries, data, inventory, excluded, pin)
	if err != nil {
		return foldkitSource{}, err
	}
	return foldkitSource{entries, inventory, data, manifest}, nil
}

// captureFoldkit writes the snapshot and corpus matrix through a staged
// directory, publishing both or neither. writeJSON is writeFoldkitJSON outside tests.
func captureFoldkit(source, repositoryRoot string, pin foldkitPin, writeJSON func(string, any) error, stdout io.Writer) error {
	read, err := readFoldkitSource(source, pin)
	if err != nil {
		return err
	}
	entries, inventory, manifest := read.entries, read.inventory, read.manifest
	output := pureJoin(repositoryRoot, foldkitSnapshotRelative)
	parent := pureParent(output)
	corpusPath := pureJoin(repositoryRoot, foldkitCorpusRelative, "manifest.json")
	if exists, err := existsOrSymlink(output); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("refusing to replace existing snapshot: %s", output)
	}
	if exists, err := existsOrSymlink(corpusPath); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("refusing to replace existing corpus manifest: %s", corpusPath)
	}
	if err := os.MkdirAll(parent, 0o777); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(parent, ".foldkit-capture-*")
	if err != nil {
		return err
	}
	if stage, err = filepath.Abs(stage); err != nil {
		return err
	}
	corpusStage := filepath.Join(filepath.Dir(stage), "."+filepath.Base(stage)+"-corpus-manifest.json")
	publishedSnapshot, publishedCorpus, published := false, false, false
	defer func() {
		if !published {
			if publishedCorpus {
				os.Remove(corpusPath)
			}
			if publishedSnapshot {
				os.RemoveAll(output)
			}
			os.RemoveAll(stage)
		}
		os.Remove(corpusStage)
	}()
	if err := foldkitWriteTree(stage, entries, read.data); err != nil {
		return err
	}
	if err := os.WriteFile(stage+"/NOTICE", []byte(foldkitNoticeContent), 0o666); err != nil {
		return err
	}
	if err := writeJSON(stage+"/manifest.json", manifest); err != nil {
		return err
	}
	corpus, err := foldkitCorpusManifestFor(output, repositoryRoot, pin)
	if err != nil {
		return err
	}
	if err := writeJSON(corpusStage, corpus); err != nil {
		return err
	}
	if err := os.Rename(stage, output); err != nil {
		return err
	}
	publishedSnapshot = true
	if err := os.MkdirAll(pureParent(corpusPath), 0o777); err != nil {
		return err
	}
	if err := os.Rename(corpusStage, corpusPath); err != nil {
		return err
	}
	publishedCorpus = true
	published = true
	integrity := manifest["integrity"].(map[string]any)
	fmt.Fprintf(stdout, "captured %d files across %d examples at %s\n", len(entries), len(inventory), output)
	fmt.Fprintf(stdout, "ROOT_SHA256=%s\n", integrity["rootSha256"])
	fmt.Fprintf(stdout, "EXAMPLE_INVENTORY_SHA256=%s\n", integrity["exampleInventorySha256"])
	fmt.Fprintf(stdout, "FILE_COUNT=%d\n", len(entries))
	return nil
}

func checkFoldkitSource(source, snapshot string, stdout io.Writer) error {
	read, err := readFoldkitSource(source, pinnedFoldkit)
	if err != nil {
		return err
	}
	actual, err := validateFoldkitSnapshot(snapshot)
	if err != nil {
		return err
	}
	if !pyEqual(read.manifest, actual) {
		return errors.New("captured Foldkit source differs from the immutable source checkout")
	}
	fmt.Fprintf(stdout, "source checkout matches %d captured files at %s\n", len(read.entries), foldkitCommit)
	return nil
}

func selfCheckFoldkit(repositoryRoot string, stdout io.Writer) error {
	snapshot := pureJoin(repositoryRoot, foldkitSnapshotRelative)
	manifest, err := validateFoldkitSnapshot(snapshot)
	if err != nil {
		return err
	}
	if err := validateFoldkitCorpusManifest(pureJoin(repositoryRoot, foldkitCorpusRelative, "manifest.json"), snapshot, repositoryRoot); err != nil {
		return err
	}
	selection, integrity := object(manifest, "selection"), object(manifest, "integrity")
	fmt.Fprintf(stdout, "verified reference-only snapshot: %d files, %d examples, %s\n",
		len(manifest["files"].([]any)), len(selection["exampleDirectories"].([]any)), pyStr(integrity["rootSha256"]))
	return nil
}
