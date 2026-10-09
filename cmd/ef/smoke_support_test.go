package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// The process smokes formerly lived in Python scripts and drove bin/ef
// from the repository root, writing dist/ there. Their Go ports drive the
// package's test CLI in a private workspace instead.

// smokeWorkspace returns a temporary workspace holding copies of the named
// repository files and directories (relative to the repository root), with
// node_modules linked to the pinned install. The copies are read in this
// process, so Go's test cache sees every input and reruns the test when one
// changes; the CLI's own reads in a subprocess would be invisible to it.
func smokeWorkspace(t *testing.T, paths ...string) string {
	t.Helper()
	// The gate names the external tool versions (Node, Bun, tsc) here, so a
	// cached pass never survives a tool upgrade.
	_ = os.Getenv("EFFRA_TOOL_VERSIONS")
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	for _, path := range paths {
		source := filepath.Join(root, filepath.FromSlash(path))
		err := filepath.WalkDir(source, func(name string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(root, name)
			if err != nil {
				return err
			}
			target := filepath.Join(workspace, relative)
			if entry.IsDir() {
				return os.MkdirAll(target, 0o755)
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			data, err := os.ReadFile(name)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			return os.WriteFile(target, data, info.Mode().Perm())
		})
		if err != nil {
			t.Fatalf("copy %s into the smoke workspace: %v", path, err)
		}
	}
	modules := filepath.Join(root, "node_modules")
	if _, err := os.Stat(modules); err != nil {
		t.Fatalf("pinned JavaScript packages: %v", err)
	}
	if err := os.Symlink(modules, filepath.Join(workspace, "node_modules")); err != nil {
		t.Fatal(err)
	}
	return workspace
}

// smokeJSON decodes one JSON document or fails the test.
func smokeJSON(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		t.Fatalf("not a JSON object: %v\n%s", err, data)
	}
	return value
}

// adapterSemantic removes only adapter envelope and byte-charge fields from a
// report.
func adapterSemantic(value map[string]any) map[string]any {
	result := map[string]any{}
	for key, item := range value {
		if key != "file" && key != "timings" {
			result[key] = item
		}
	}
	if usage, ok := result["typeProjectionUsage"].(map[string]any); ok {
		kept := map[string]any{}
		for key, item := range usage {
			if key != "compatibilityBytes" && key != "nameBytes" && key != "responseBytes" {
				kept[key] = item
			}
		}
		result["typeProjectionUsage"] = kept
	}
	return result
}

// Current wire epochs of a decorated report and of its embedded semantic
// snapshot; the two advance independently.
const (
	defaultReportSchema   = 9
	defaultSnapshotSchema = 9
)

var artifactDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// producerSnapshot validates one response envelope and its independent
// snapshot epoch and returns its producer identity, failing the test when the
// response is malformed. An empty target accepts the response's own.
func producerSnapshot(t *testing.T, value map[string]any, target string, snapshotSchema int) map[string]any {
	t.Helper()
	producer, err := checkProducerSnapshot(value, target, snapshotSchema)
	if err != nil {
		t.Fatal(err)
	}
	return producer
}

// checkProducerSnapshot is producerSnapshot reporting a malformed response as
// an error, so a control can observe the oracle refusing one.
func checkProducerSnapshot(value map[string]any, target string, snapshotSchema int) (map[string]any, error) {
	for _, key := range []string{"schemaVersion", "revision", "target", "producer", "snapshot"} {
		if _, ok := value[key]; !ok {
			return nil, fmt.Errorf("decorated response lacks %s: %v", key, value)
		}
	}
	expectedTarget := value["target"]
	if target != "" {
		expectedTarget = target
	}
	if value["target"] != expectedTarget {
		return nil, fmt.Errorf("target = %v, want %v", value["target"], expectedTarget)
	}
	producer, ok := value["producer"].(map[string]any)
	snapshot, ok2 := value["snapshot"].(map[string]any)
	if !ok || !ok2 {
		return nil, fmt.Errorf("producer and snapshot must be objects: %v", value)
	}
	if !positiveJSONInt(value["schemaVersion"]) {
		return nil, fmt.Errorf("report schema %#v is not a positive integer", value["schemaVersion"])
	}
	if snapshotSchema <= 0 {
		return nil, fmt.Errorf("expected snapshot schema %d is not positive", snapshotSchema)
	}
	for _, key := range []string{"strength", "qualifier", "reuseScope"} {
		if _, ok := producer[key]; !ok {
			return nil, fmt.Errorf("producer lacks %s: %v", key, producer)
		}
	}
	scope := producer["reuseScope"]
	switch scope {
	case "artifact":
		digest, _ := producer["digest"].(string)
		if producer["strength"] != "executing-artifact" || !artifactDigest.MatchString(digest) || producer["qualifier"] != digest {
			return nil, fmt.Errorf("artifact producer: %v", producer)
		}
		if reason, ok := producer["reason"]; ok && reason != "" {
			return nil, fmt.Errorf("artifact producer carries a reason: %v", producer)
		}
	case "process", "none":
		if producer["strength"] != "unavailable" {
			return nil, fmt.Errorf("unavailable producer: %v", producer)
		}
		if digest, ok := producer["digest"]; ok && digest != "" {
			return nil, fmt.Errorf("unavailable producer carries a digest: %v", producer)
		}
		if reason, _ := producer["reason"].(string); reason == "" {
			return nil, fmt.Errorf("unavailable producer lacks a reason: %v", producer)
		}
		qualifier, _ := producer["qualifier"].(string)
		if scope == "process" && (!strings.HasPrefix(qualifier, "process:") || len(qualifier) <= len("process:")) {
			return nil, fmt.Errorf("process producer qualifier: %v", producer)
		}
		if scope == "none" && qualifier != "" {
			return nil, fmt.Errorf("none producer qualifier: %v", producer)
		}
	default:
		return nil, fmt.Errorf("reuse scope %v", scope)
	}
	if observed := snapshot["schemaVersion"]; !positiveJSONInt(observed) || observed != float64(snapshotSchema) {
		return nil, fmt.Errorf("snapshot schema = %#v, want %d", observed, snapshotSchema)
	}
	want := map[string]any{
		"schemaVersion": float64(snapshotSchema),
		"revision":      value["revision"],
		"target":        expectedTarget,
		"producer":      producer["qualifier"],
		"reuseScope":    scope,
	}
	if !reflect.DeepEqual(snapshot, want) {
		return nil, fmt.Errorf("snapshot = %v, want %v", snapshot, want)
	}
	return producer, nil
}

// positiveJSONInt reports whether a decoded JSON value is a positive integer:
// a boolean or fractional number is not.
func positiveJSONInt(value any) bool {
	number, ok := value.(float64)
	return ok && number > 0 && number == math.Trunc(number)
}

// parityOptions are assertReportParity's optional projections.
type parityOptions struct {
	target         string
	ignored        []string
	project        func(map[string]any) map[string]any
	reportSchema   int // default defaultReportSchema
	snapshotSchema int // default defaultSnapshotSchema
}

// assertReportParity compares decorated reports without erasing their
// producer contract. Artifact-scoped reports are equal after only explicitly
// named adapter envelope projections; process-scoped reports may differ only
// in the process qualifier; "none" reports retain exact metadata equality.
// The outer report and the embedded semantic snapshot have separate epochs.
func assertReportParity(t *testing.T, actual, expected map[string]any, options parityOptions) {
	t.Helper()
	if err := checkReportParity(actual, expected, options); err != nil {
		t.Fatal(err)
	}
}

// checkReportParity is assertReportParity reporting a mismatch as an error,
// so a control can observe the oracle refusing a malformed report.
func checkReportParity(actual, expected map[string]any, options parityOptions) error {
	if options.reportSchema == 0 {
		options.reportSchema = defaultReportSchema
	}
	if options.snapshotSchema == 0 {
		options.snapshotSchema = defaultSnapshotSchema
	}
	for _, report := range []map[string]any{actual, expected} {
		if report["schemaVersion"] != float64(options.reportSchema) {
			return fmt.Errorf("report schema = %#v, want %d: %v", report["schemaVersion"], options.reportSchema, report)
		}
	}
	actualProducer, err := checkProducerSnapshot(actual, options.target, options.snapshotSchema)
	if err != nil {
		return err
	}
	expectedProducer, err := checkProducerSnapshot(expected, options.target, options.snapshotSchema)
	if err != nil {
		return err
	}
	for _, key := range []string{"schemaVersion", "revision", "target"} {
		if !reflect.DeepEqual(actual[key], expected[key]) {
			return fmt.Errorf("%s: %v != %v", key, actual[key], expected[key])
		}
	}
	for _, key := range []string{"source", "sources"} {
		left, inLeft := actual[key]
		right, inRight := expected[key]
		if inLeft != inRight || !reflect.DeepEqual(left, right) {
			return fmt.Errorf("%s: %v != %v", key, left, right)
		}
	}
	if actualProducer["reuseScope"] != expectedProducer["reuseScope"] {
		return fmt.Errorf("reuse scopes differ: %v / %v", actualProducer, expectedProducer)
	}
	project := func(value map[string]any) (map[string]any, error) {
		result, err := copyJSON(value)
		if err != nil {
			return nil, err
		}
		for _, key := range options.ignored {
			delete(result, key)
		}
		if options.project != nil {
			result = options.project(result)
		}
		return result, nil
	}
	left, err := project(actual)
	if err != nil {
		return err
	}
	right, err := project(expected)
	if err != nil {
		return err
	}
	if actualProducer["reuseScope"] == "process" {
		for _, report := range []map[string]any{left, right} {
			report["producer"].(map[string]any)["qualifier"] = "<process>"
			report["snapshot"].(map[string]any)["producer"] = "<process>"
		}
	}
	if !reflect.DeepEqual(left, right) {
		leftJSON, _ := json.Marshal(left)
		rightJSON, _ := json.Marshal(right)
		return fmt.Errorf("reports differ:\n%s\n%s", leftJSON, rightJSON)
	}
	return nil
}

// copyJSON copies a decoded JSON object through its encoding.
func copyJSON(value map[string]any) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// deepCopyJSON copies a decoded JSON object.
func deepCopyJSON(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	result, err := copyJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// smokeWaitDelay bounds how long a killed or exited smoke child may keep its
// output pipes open through a surviving grandchild before Wait closes them.
const smokeWaitDelay = 5 * time.Second

// errSmokeDeadline marks a smoke process that outlived its deadline.
var errSmokeDeadline = errors.New("smoke process exceeded its deadline")

// runSmokeDeadline runs an unstarted command to completion under a
// per-operation deadline, as the Python smokes' subprocess.run(timeout=...)
// did, and returns its output and exit code. On expiry the child is killed
// and reaped before an errSmokeDeadline error returns. It never touches a
// testing.T, so concurrent runners may use it.
func runSmokeDeadline(command *exec.Cmd, timeout time.Duration) ([]byte, []byte, int, error) {
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if command.WaitDelay == 0 {
		command.WaitDelay = smokeWaitDelay
	}
	if err := command.Start(); err != nil {
		return nil, nil, -1, err
	}
	// This goroutine is the command's only wait owner; done closes once the
	// child has been reaped and its output copied.
	var err error
	done := make(chan struct{})
	go func() {
		err = command.Wait()
		close(done)
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		_ = command.Process.Kill()
		<-done
		return stdout.Bytes(), stderr.Bytes(), -1, fmt.Errorf("%w: %s %s after %v\nstdout=%q\nstderr=%q",
			errSmokeDeadline, command.Path, strings.Join(command.Args[1:], " "), timeout, stdout.Bytes(), stderr.Bytes())
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return stdout.Bytes(), stderr.Bytes(), exit.ExitCode(), nil
	}
	if err != nil {
		return stdout.Bytes(), stderr.Bytes(), -1, err
	}
	return stdout.Bytes(), stderr.Bytes(), 0, nil
}

// runSmokeCommand is runSmokeDeadline failing the test when the command
// cannot run or outlives its deadline.
func runSmokeCommand(t *testing.T, timeout time.Duration, command *exec.Cmd) ([]byte, []byte, int) {
	t.Helper()
	stdout, stderr, code, err := runSmokeDeadline(command, timeout)
	if err != nil {
		t.Fatal(err)
	}
	return stdout, stderr, code
}

// runSmokeCLI runs the CLI in directory with input on stdin under a
// per-operation deadline.
func runSmokeCLI(t *testing.T, timeout time.Duration, binary, directory, input string, args ...string) ([]byte, []byte, int) {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Dir = directory
	command.Stdin = strings.NewReader(input)
	return runSmokeCommand(t, timeout, command)
}

// smokeObject is a JSON object whose members keep their written order, as
// the members of a Python dict literal do, so a request built from it
// encodes to the exact bytes the Python smoke sent.
type smokeObject []smokeMember

type smokeMember struct {
	key   string
	value any
}

func (object smokeObject) MarshalJSON() ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	buffer.WriteByte('{')
	for index, member := range object {
		if index > 0 {
			buffer.WriteByte(',')
		}
		if err := encoder.Encode(member.key); err != nil {
			return nil, err
		}
		buffer.Truncate(buffer.Len() - 1)
		buffer.WriteByte(':')
		if err := encoder.Encode(member.value); err != nil {
			return nil, err
		}
		buffer.Truncate(buffer.Len() - 1)
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}

// smokeDumps encodes a request as Python's json.dumps(value): characters
// outside printable ASCII as \uXXXX escapes (astral ones as UTF-16 surrogate
// pairs), <, > and & literal, and ", " and ": " separators. A map's members
// are ordered by key, which no server's decoding depends on; a smokeObject
// keeps its written order and so reproduces Python's bytes exactly.
func smokeDumps(t *testing.T, value any) []byte {
	t.Helper()
	return smokePythonJSON(t, value, true, true)
}

// smokeDumpsUTF8 encodes as json.dumps(value, ensure_ascii=False): text
// stays literal UTF-8, line and paragraph separators included.
func smokeDumpsUTF8(t *testing.T, value any) []byte {
	t.Helper()
	return smokePythonJSON(t, value, false, true)
}

// smokeDumpsCompact encodes as json.dumps(value, separators=(",", ":")).
func smokeDumpsCompact(t *testing.T, value any) []byte {
	t.Helper()
	return smokePythonJSON(t, value, true, false)
}

func smokePythonJSON(t *testing.T, value any, asciiOnly, spaced bool) []byte {
	t.Helper()
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		t.Fatal(err)
	}
	return pythonJSON(bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), asciiOnly, spaced)
}

// pythonJSON rewrites Go's compact, HTML-unescaped JSON to Python's
// spelling. Both escape quote, backslash and C0 controls alike. Go also
// escapes U+2028 and U+2029, which Python leaves literal unless asciiOnly,
// where Python escapes every character outside printable ASCII. Python
// separates members and items with ", " and keys with ": " unless compact.
func pythonJSON(encoded []byte, asciiOnly, spaced bool) []byte {
	var out bytes.Buffer
	inString := false
	for index := 0; index < len(encoded); {
		c := encoded[index]
		switch {
		case !inString:
			inString = c == '"'
			out.WriteByte(c)
			if spaced && (c == ',' || c == ':') {
				out.WriteByte(' ')
			}
			index++
		case c == '\\' && encoded[index+1] == 'u':
			escape := string(encoded[index : index+6])
			if code, _ := strconv.ParseUint(escape[2:], 16, 16); !asciiOnly && (code == 0x2028 || code == 0x2029) {
				out.WriteRune(rune(code))
			} else {
				out.WriteString(escape)
			}
			index += 6
		case c == '\\':
			out.Write(encoded[index : index+2])
			index += 2
		case c == '"':
			inString = false
			out.WriteByte(c)
			index++
		case c == 0x7f && asciiOnly:
			fmt.Fprintf(&out, "\\u%04x", c)
			index++
		case c < utf8.RuneSelf || !asciiOnly:
			out.WriteByte(c)
			index++
		default:
			r, size := utf8.DecodeRune(encoded[index:])
			if r >= 0x10000 {
				high, low := utf16.EncodeRune(r)
				fmt.Fprintf(&out, "\\u%04x\\u%04x", high, low)
			} else {
				fmt.Fprintf(&out, "\\u%04x", r)
			}
			index += size
		}
	}
	return out.Bytes()
}
