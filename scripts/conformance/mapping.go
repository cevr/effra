package main

// Validate the selected Effect behavior mapping. Each case records where its
// behavior is specified upstream (file, line and test label at the pinned
// commit) as provenance text; Effra keeps no copy of the upstream corpus, so
// the record is checked for shape, and its evidence must be an existing Go
// test in internal/compiler.

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

const mappingRelative = "conformance/effect-cases.json"

var (
	mappingStatuses = []string{"covered", "difference", "pending", "unsupported"}
	mappingCaseID   = regexp.MustCompile(`^effect\.[a-z0-9.-]+$`)
	mappingTestName = regexp.MustCompile(`^Test[A-Za-z0-9_]+$`)
)

// upstreamTestComponents are the path components that mark an upstream file
// as a test, as the pinned selection rule chose them.
var upstreamTestComponents = []string{"test", "tests", "typetest", "test-dts"}

func readMapping(path string) (map[string]any, error) {
	text, err := readText(path)
	var value any
	if err == nil {
		value, err = decodeJSON(text)
	}
	if err != nil {
		return nil, errors.New("mapping is not valid UTF-8 JSON")
	}
	mapping, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("mapping root must be an object")
	}
	return mapping, nil
}

// hasExactKeys is Python's set(value) == {keys...} for a json.loads object.
func hasExactKeys(value any, keys ...string) bool {
	object, ok := value.(map[string]any)
	if !ok || len(object) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			return false
		}
	}
	return true
}

// positiveLine returns a json.loads int (never a bool or float) when it is at
// least 1, saturating lines too large for any file.
func positiveLine(value any) (int, bool) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	line, ok := jsonInt(number)
	if !ok || line.Sign() < 1 {
		return 0, false
	}
	if !line.IsInt64() || line.Int64() > int64(^uint(0)>>1) {
		return int(^uint(0) >> 1), true
	}
	return int(line.Int64()), true
}

// validateMapping validates the mapping and its evidence tests under root and
// returns the sorted evidence test names.
func validateMapping(mapping map[string]any, root string) ([]string, error) {
	if !hasExactKeys(mapping, "schemaVersion", "sourceCommit", "cases") || pyType(mapping["schemaVersion"]) != "int" || !isPyInt(mapping["schemaVersion"], 1) || mapping["sourceCommit"] != pinnedCommit {
		return nil, errors.New("mapping schema or upstream pin is invalid")
	}
	cases, ok := mapping["cases"].([]any)
	if !ok || len(cases) == 0 {
		return nil, errors.New("mapping cases must be a nonempty list")
	}
	ids := map[string]bool{}
	type upstreamCase struct {
		path string
		line int
	}
	upstreamCases := map[upstreamCase]bool{}
	tests := map[string]bool{}
	for _, value := range cases {
		if !hasExactKeys(value, "id", "status", "upstream", "behavior", "limits", "evidence") {
			return nil, errors.New("mapping case fields are invalid")
		}
		item := value.(map[string]any)
		identity, ok := item["id"].(string)
		if !ok || !mappingCaseID.MatchString(identity) || ids[identity] {
			return nil, errors.New("mapping case id is invalid or duplicated")
		}
		ids[identity] = true
		status, ok := item["status"].(string)
		if !ok || !slices.Contains(mappingStatuses, status) {
			return nil, fmt.Errorf("invalid status for %s", identity)
		}
		for _, key := range []string{"behavior", "limits"} {
			if text, ok := item[key].(string); !ok || pyStrip(text) == "" {
				return nil, fmt.Errorf("behavior and limits are required for %s", identity)
			}
		}
		if !hasExactKeys(item["upstream"], "file", "line", "label") {
			return nil, fmt.Errorf("invalid upstream case for %s", identity)
		}
		upstream := item["upstream"].(map[string]any)
		path, err := safeRelativePath(upstream["file"])
		if err != nil {
			return nil, err
		}
		line, lineOK := positiveLine(upstream["line"])
		label, labelOK := upstream["label"].(string)
		if !isUpstreamTest(path) || !lineOK || !labelOK || pyStrip(label) == "" {
			return nil, fmt.Errorf("upstream target is invalid for %s", identity)
		}
		key := upstreamCase{path, line}
		if upstreamCases[key] {
			return nil, fmt.Errorf("duplicate upstream case for %s", identity)
		}
		upstreamCases[key] = true
		evidence, ok := item["evidence"].([]any)
		if !ok || (len(evidence) > 0) != (status == "covered" || status == "difference") {
			return nil, fmt.Errorf("status/evidence misrepresentation for %s", identity)
		}
		for _, value := range evidence {
			if !hasExactKeys(value, "file", "test", "targets") {
				return nil, fmt.Errorf("invalid evidence for %s", identity)
			}
			pointer := value.(map[string]any)
			file, err := safeRelativePath(pointer["file"])
			if err != nil {
				return nil, err
			}
			test, testOK := pointer["test"].(string)
			if !strings.HasPrefix(file, "internal/compiler/") || !strings.HasSuffix(file, "_test.go") || !testOK || !mappingTestName.MatchString(test) || !isGoJSTargets(pointer["targets"]) {
				return nil, fmt.Errorf("evidence must name an existing shared Go/JS test for %s", identity)
			}
			contents, err := readText(filepath.Join(root, file))
			if err != nil {
				return nil, fmt.Errorf("evidence file is unavailable for %s", identity)
			}
			declaration := regexp.MustCompile(`(?m)^func ` + regexp.QuoteMeta(test) + `\(t \*testing\.T\) \{`)
			if !declaration.MatchString(contents) {
				return nil, fmt.Errorf("evidence test is unavailable for %s", identity)
			}
			tests[test] = true
		}
	}
	names := make([]string, 0, len(tests))
	for test := range tests {
		names = append(names, test)
	}
	sort.Strings(names)
	return names, nil
}

// isUpstreamTest reports whether an upstream path names a test file: a path
// with a test component below it.
func isUpstreamTest(path string) bool {
	parts := pathParts(path)
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts[:len(parts)-1] {
		if slices.Contains(upstreamTestComponents, part) {
			return true
		}
	}
	return false
}

// isGoJSTargets is Python's value == ["go", "js"].
func isGoJSTargets(value any) bool {
	targets, ok := value.([]any)
	return ok && len(targets) == 2 && targets[0] == "go" && targets[1] == "js"
}
