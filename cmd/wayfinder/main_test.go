package main

// Controls for the wayfinder command, driven as a separate process. The test binary doubles as the command
// (WAYFINDER_TEST_CLI=1) and, when invoked through a symlink named gh, as a fake `gh` that serves fixture
// GraphQL pages, so no test touches the network.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const testRepo = "acme/widgets"

func TestMain(m *testing.M) {
	if filepath.Base(os.Args[0]) == "gh" {
		os.Exit(fakeGH(os.Args[1:]))
	}
	if os.Getenv("WAYFINDER_TEST_CLI") == "1" {
		os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

// fakeGH serves read-only GraphQL from the JSON state named by FAKE_GH_STATE and logs each call.
func fakeGH(args []string) int {
	log, err := os.OpenFile(os.Getenv("FAKE_GH_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		line, _ := json.Marshal(args)
		fmt.Fprintln(log, string(line))
		log.Close()
	}
	if len(args) < 2 || args[0] != "api" || args[1] != "graphql" {
		fmt.Fprintln(os.Stderr, "fake gh only serves read-only graphql")
		return 1
	}
	fields := map[string]string{}
	for index := 3; index < len(args); index += 2 {
		key, value, _ := strings.Cut(args[index], "=")
		fields[key] = value
	}
	if !strings.HasPrefix(strings.TrimLeft(fields["query"], " \t\n"), "query(") {
		fmt.Fprintln(os.Stderr, "fake gh refuses mutations")
		return 1
	}
	var state struct {
		Nodes    []map[string]any `json:"nodes"`
		PageSize int              `json:"pageSize"`
		Comments map[string][]any `json:"comments"`
	}
	data, _ := os.ReadFile(os.Getenv("FAKE_GH_STATE"))
	if err := json.Unmarshal(data, &state); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var response any
	if number, ok := fields["number"]; ok {
		for _, node := range state.Nodes {
			if fmt.Sprint(node["number"]) == number {
				comments := state.Comments[number]
				if comments == nil {
					comments = []any{}
				}
				response = map[string]any{"repository": map[string]any{"issue": map[string]any{
					"body": node["body"], "comments": map[string]any{"totalCount": len(comments), "nodes": comments},
				}}}
			}
		}
	} else {
		start, _ := strconv.Atoi(fields["cursor"])
		end := min(start+state.PageSize, len(state.Nodes))
		response = map[string]any{"repository": map[string]any{"issues": map[string]any{
			"pageInfo": map[string]any{"hasNextPage": end < len(state.Nodes), "endCursor": strconv.Itoa(end)},
			"nodes":    state.Nodes[start:end],
		}}}
	}
	encoded, _ := json.Marshal(map[string]any{"data": response})
	fmt.Println(string(encoded))
	return 0
}

// Fixtures --------------------------------------------------------------------------------------------------

func conn(key string, values ...any) map[string]any {
	nodes := make([]any, len(values))
	for index, value := range values {
		nodes[index] = map[string]any{key: value}
	}
	return map[string]any{"totalCount": len(values), "nodes": nodes}
}

type spec struct {
	labels    []string
	parent    int // 0: none
	blocked   []any
	closed    bool
	assignees []any
	body      string
}

func node(number int, s spec) map[string]any {
	state, reason := "OPEN", any(nil)
	if s.closed {
		state, reason = "CLOSED", "COMPLETED"
	}
	var parent any
	if s.parent != 0 {
		parent = map[string]any{"number": s.parent}
	}
	labels := make([]any, len(s.labels))
	for index, label := range s.labels {
		labels[index] = label
	}
	return map[string]any{
		"number": number, "title": fmt.Sprintf("Issue %d", number), "state": state, "stateReason": reason,
		"url": fmt.Sprintf("https://github.com/%s/issues/%d", testRepo, number), "updatedAt": "2026-10-09T00:00:00Z",
		"body": s.body, "labels": conn("name", labels...), "assignees": conn("login", s.assignees...),
		"parent": parent, "blockedBy": conn("number", s.blocked...),
	}
}

var task, grill = []string{"wayfinder:task"}, []string{"wayfinder:grilling"}

// fixture is a map with one ticket per derived status, plus issues outside the map.
func fixture() []map[string]any {
	return []map[string]any{
		node(1, spec{labels: []string{"wayfinder:map"}, body: "See #2, #9 and https://github.com/acme/widgets/issues/4."}),
		node(2, spec{labels: task, parent: 1}),                                              // ready leaf
		node(3, spec{labels: task, parent: 1, assignees: []any{"ana"}}),                     // claimed
		node(4, spec{labels: task, parent: 1, blocked: []any{2}}),                           // blocked by open #2
		node(5, spec{labels: task, parent: 1, blocked: []any{6}}),                           // blocker closed: ready
		node(6, spec{labels: task, parent: 1, closed: true}),                                //
		node(7, spec{labels: grill, parent: 1}),                                             // HITL: frontier only with --all
		node(8, spec{labels: []string{"wayfinder:task", "implementation:spec"}, parent: 1}), // rollup with open child 9
		node(9, spec{labels: task, parent: 8}),                                              //
		node(10, spec{labels: task, parent: 1}),                                             // children all closed: ready
		node(11, spec{labels: task, parent: 10, closed: true}),                              //
		node(12, spec{labels: task, closed: true}),                                          // closed duplicate outside the map
		node(13, spec{labels: task}),                                                        // open stray outside the map
		node(14, spec{labels: []string{"bug"}}),                                             // unrelated repository issue
	}
}

type record map[string]any

func rec(number int, labels []string, parent any, blocked []any, state string) record {
	sorted := append([]string(nil), labels...)
	sort.Strings(sorted)
	labelValues := make([]any, len(sorted))
	for index, label := range sorted {
		labelValues[index] = label
	}
	if blocked == nil {
		blocked = []any{}
	}
	return record{
		"number": number, "title": fmt.Sprintf("Issue %d", number), "state": state, "stateReason": nil, "labels": labelValues,
		"assignees": []any{}, "parent": parent, "blockedBy": blocked, "url": fmt.Sprintf("https://github.com/%s/issues/%d", testRepo, number),
		"updatedAt": "2026-10-09T00:00:00Z", "bodySha256": strings.Repeat("0", 64),
	}
}

func task2(number int) record { return rec(number, task, 1, nil, "open") }

func doc(references []any, issues ...record) map[string]any {
	all := []any{map[string]any(rec(1, []string{"wayfinder:map"}, nil, nil, "open"))}
	for _, issue := range issues {
		all = append(all, map[string]any(issue))
	}
	if references == nil {
		references = []any{}
	}
	return map[string]any{"schema": "effra-wayfinder-snapshot/1", "repository": testRepo, "map": 1, "mapReferences": references, "issues": all}
}

// canonical is the published snapshot format: two-space indentation, sorted keys, trailing newline.
func canonical(value any) string { return renderJSON(value, true) + "\n" }

// Harness ---------------------------------------------------------------------------------------------------

type harness struct {
	t                   *testing.T
	tmp, bin, emptyBin  string
	state, log, testBin string
}

type result struct {
	code           int
	stdout, stderr string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	tmp := t.TempDir()
	h := &harness{t: t, tmp: tmp, bin: filepath.Join(tmp, "bin"), emptyBin: filepath.Join(tmp, "empty"),
		state: filepath.Join(tmp, "state.json"), log: filepath.Join(tmp, "gh.log")}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	h.testBin = executable
	for _, directory := range []string{h.bin, h.emptyBin} {
		if err := os.Mkdir(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(executable, filepath.Join(h.bin, "gh")); err != nil {
		t.Fatal(err)
	}
	h.serve(fixture(), nil)
	return h
}

func (h *harness) serve(nodes []map[string]any, comments map[string]any) {
	h.t.Helper()
	if comments == nil {
		comments = map[string]any{}
	}
	data, _ := json.Marshal(map[string]any{"nodes": nodes, "pageSize": 4, "comments": comments})
	if err := os.WriteFile(h.state, data, 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) runWith(path string, args ...string) result {
	h.t.Helper()
	command := exec.Command(h.testBin, args...)
	command.Dir = h.tmp
	command.Env = append(os.Environ(), "WAYFINDER_TEST_CLI=1", "PATH="+path, "FAKE_GH_STATE="+h.state, "FAKE_GH_LOG="+h.log)
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		h.t.Fatal(err)
	}
	return result{code, stdout.String(), stderr.String()}
}

func (h *harness) live(args ...string) result {
	return h.runWith(h.bin, append(args, "--repo", testRepo)...)
}

func (h *harness) offline(args ...string) result { return h.runWith(h.emptyBin, args...) }

func (h *harness) jsonNumbers(args ...string) []int {
	h.t.Helper()
	out := h.live(append(args, "--json")...)
	if out.code != 0 {
		h.t.Fatalf("exit %d: %s", out.code, out.stderr)
	}
	return numbers(h.t, out.stdout)
}

func numbers(t *testing.T, text string) []int {
	t.Helper()
	var issues []map[string]any
	if err := json.Unmarshal([]byte(text), &issues); err != nil {
		t.Fatalf("%v: %s", err, text)
	}
	result := []int{}
	for _, issue := range issues {
		result = append(result, int(issue["number"].(float64)))
	}
	return result
}

func (h *harness) writeSnapshot(value any) string {
	h.t.Helper()
	text, ok := value.(string)
	if !ok {
		text = canonical(value)
	}
	path := filepath.Join(h.tmp, "snapshot.json")
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		h.t.Fatal(err)
	}
	return path
}

func (h *harness) offlineCheck(value any) result {
	return h.offline("check", "--snapshot", h.writeSnapshot(value))
}

func expectInts(t *testing.T, got, want []int) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func expectContains(t *testing.T, text, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Fatalf("missing %q in:\n%s", want, text)
	}
}

func expectFailure(t *testing.T, out result, want string) {
	t.Helper()
	if out.code != 1 {
		t.Fatalf("exit %d, want 1; stderr:\n%s", out.code, out.stderr)
	}
	expectContains(t, out.stderr, want)
}

// Frontier and derived status -------------------------------------------------------------------------------

func TestFrontierIsOpenUnclaimedUnblockedNonHITLLeaves(t *testing.T) {
	expectInts(t, newHarness(t).jsonNumbers("frontier"), []int{2, 5, 9, 10})
}

func TestFrontierAllAddsHITLTicketsOnly(t *testing.T) {
	expectInts(t, newHarness(t).jsonNumbers("frontier", "--all"), []int{2, 5, 7, 9, 10})
}

func TestListDerivesEveryStatusAndExcludesIssuesOutsideTheMap(t *testing.T) {
	out := newHarness(t).live("list", "--json")
	var issues []map[string]any
	if err := json.Unmarshal([]byte(out.stdout), &issues); err != nil {
		t.Fatal(err)
	}
	statuses := map[int]string{}
	for _, issue := range issues {
		statuses[int(issue["number"].(float64))] = issue["status"].(string)
	}
	want := map[int]string{2: "ready", 3: "claimed", 4: "blocked", 5: "ready", 6: "closed", 7: "hitl", 8: "rollup", 9: "ready", 10: "ready", 11: "closed"}
	if !reflect.DeepEqual(statuses, want) {
		t.Fatalf("got %v, want %v", statuses, want)
	}
}

func TestFrontierFollowsGitHubWhenABlockerCloses(t *testing.T) {
	h := newHarness(t)
	nodes := fixture()
	nodes[1]["state"] = "CLOSED" // close #2, which blocks #4
	h.serve(nodes, nil)
	expectInts(t, h.jsonNumbers("frontier"), []int{4, 5, 9, 10})
}

func TestTextOutputNamesBlockersClaimsAndOpenChildren(t *testing.T) {
	out := newHarness(t).live("list")
	expectContains(t, out.stdout, "#4    blocked  task       Issue 4 (blocked by #2)")
	expectContains(t, out.stdout, "(claimed by @ana)")
	expectContains(t, out.stdout, "(open children #9)")
}

// Snapshot --------------------------------------------------------------------------------------------------

func TestSnapshotIsADeterministicProjectionOfTheMapTree(t *testing.T) {
	h := newHarness(t)
	first, second := filepath.Join(h.tmp, "first.json"), filepath.Join(h.tmp, "second.json")
	if out := h.live("snapshot", "--output", first); out.code != 0 {
		t.Fatal(out.stderr)
	}
	reversed := fixture()
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	h.serve(reversed, nil) // different page order, same GitHub state
	if out := h.live("snapshot", "--output", second); out.code != 0 {
		t.Fatal(out.stderr)
	}
	a, _ := os.ReadFile(first)
	b, _ := os.ReadFile(second)
	if !bytes.Equal(a, b) {
		t.Fatalf("snapshots differ:\n%s\n%s", a, b)
	}
	var snapshot struct {
		Issues        []map[string]any `json:"issues"`
		MapReferences []int            `json:"mapReferences"`
	}
	if err := json.Unmarshal(a, &snapshot); err != nil {
		t.Fatal(err)
	}
	var got []int
	for _, issue := range snapshot.Issues {
		got = append(got, int(issue["number"].(float64)))
	}
	expectInts(t, got, []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11})
	expectInts(t, snapshot.MapReferences, []int{2, 4, 9})
	if snapshot.Issues[5]["stateReason"] != "completed" {
		t.Fatalf("stateReason = %v", snapshot.Issues[5]["stateReason"])
	}
	if _, ok := snapshot.Issues[0]["body"]; ok {
		t.Fatal("snapshot must not carry bodies")
	}
}

func TestSnapshotPaginatesWithReadOnlyQueries(t *testing.T) {
	h := newHarness(t)
	h.live("snapshot", "--output", filepath.Join(h.tmp, "out.json"))
	data, _ := os.ReadFile(h.log)
	var cursors []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var args []string
		if err := json.Unmarshal([]byte(line), &args); err != nil {
			t.Fatal(err)
		}
		cursor := "none"
		for index := 3; index < len(args); index += 2 {
			if value, ok := strings.CutPrefix(args[index], "cursor="); ok {
				cursor = value
			}
		}
		cursors = append(cursors, cursor)
	}
	// 14 issues, 4 per page.
	if want := []string{"none", "4", "8", "12"}; !reflect.DeepEqual(cursors, want) {
		t.Fatalf("cursors %v, want %v", cursors, want)
	}
}

func TestSnapshotCheckReportsStalenessWithoutWriting(t *testing.T) {
	h := newHarness(t)
	path := filepath.Join(h.tmp, "snapshot.json")
	h.live("snapshot", "--output", path)
	if out := h.live("snapshot", "--check", "--output", path); out.code != 0 {
		t.Fatal(out.stderr)
	}
	nodes := fixture()
	nodes[2]["assignees"] = conn("login")
	h.serve(nodes, nil)
	before, _ := os.ReadFile(path)
	expectFailure(t, h.live("snapshot", "--check", "--output", path), "is stale")
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("snapshot --check wrote the file")
	}
}

func TestSnapshotRefusesAStructurallyInvalidMap(t *testing.T) {
	h := newHarness(t)
	nodes := fixture()
	nodes[1]["blockedBy"] = conn("number", 4) // #2 <-> #4
	h.serve(nodes, nil)
	path := filepath.Join(h.tmp, "snapshot.json")
	expectFailure(t, h.live("snapshot", "--output", path), "blocked-by cycle: #2 -> #4 -> #2")
	if _, err := os.Stat(path); err == nil {
		t.Fatal("refused snapshot was written")
	}
}

func TestSnapshotWrittenLiveReadsBackOfflineWithoutGH(t *testing.T) {
	h := newHarness(t)
	path := filepath.Join(h.tmp, "snapshot.json")
	h.live("snapshot", "--output", path)
	if out := h.offline("check", "--snapshot", path); out.code != 0 {
		t.Fatal(out.stderr)
	}
	expectInts(t, numbers(t, h.offline("frontier", "--json", "--snapshot", path).stdout), []int{2, 5, 9, 10})
}

// Check -----------------------------------------------------------------------------------------------------

func TestLiveCheckWarnsAboutOpenWayfinderIssuesOutsideTheMap(t *testing.T) {
	out := newHarness(t).live("check")
	if out.code != 0 {
		t.Fatal(out.stderr)
	}
	expectContains(t, out.stderr, "warning: #13 is open with Wayfinder labels but is not under the map")
	for _, absent := range []string{"#12", "#14"} {
		if strings.Contains(out.stderr, absent) {
			t.Fatalf("unexpected %s in:\n%s", absent, out.stderr)
		}
	}
}

func TestValidSnapshotPasses(t *testing.T) {
	out := newHarness(t).offlineCheck(doc([]any{2}, task2(2), rec(3, task, 1, []any{2}, "open")))
	if out.code != 0 {
		t.Fatal(out.stderr)
	}
	expectContains(t, out.stdout, "3 issues valid")
}

func TestStructuralErrorsFail(t *testing.T) {
	cases := []struct {
		want   string
		issues []record
	}{
		{"blocked-by cycle: #2 -> #3 -> #2", []record{rec(2, task, 1, []any{3}, "open"), rec(3, task, 1, []any{2}, "open")}},
		{"#3 is open under closed parent #2", []record{rec(2, task, 1, nil, "closed"), rec(3, task, 2, nil, "open")}},
		{"#2 needs exactly one type label", []record{rec(2, []string{"implementation:task"}, 1, nil, "open")}},
		{"#2 needs exactly one type label from", []record{rec(2, []string{"wayfinder:task", "wayfinder:research"}, 1, nil, "open")}},
		{"#2 is blocked by #40, which is not in the map", []record{rec(2, task, 1, []any{40}, "open")}},
		{"#2 parent #40 is not in the map", []record{rec(2, task, 40, nil, "open")}},
		{"parent cycle: #2 -> #3 -> #2", []record{rec(2, task, 3, nil, "open"), rec(3, task, 2, nil, "open")}},
		{"exactly #1 must carry wayfinder:map, found #1,#2", []record{rec(2, []string{"wayfinder:map", "wayfinder:task"}, 1, nil, "open")}},
	}
	h := newHarness(t)
	for _, test := range cases {
		t.Run(test.want, func(t *testing.T) {
			expectFailure(t, h.offlineCheck(doc(nil, test.issues...)), test.want)
		})
	}
}

func TestMapBodyReferenceToAMissingIssueFails(t *testing.T) {
	expectFailure(t, newHarness(t).offlineCheck(doc([]any{2, 99}, task2(2))), "map body references #99")
}

func TestClosedIssueWithAnOpenBlockerIsAStaleEdgeWarning(t *testing.T) {
	out := newHarness(t).offlineCheck(doc(nil, task2(2), rec(3, task, 1, []any{2}, "closed")))
	if out.code != 0 {
		t.Fatal(out.stderr)
	}
	expectContains(t, out.stderr, "warning: #3 is closed but still blocked by open #2")
}

func TestSchemaViolationsFailBeforeStructure(t *testing.T) {
	compact, _ := json.Marshal(doc(nil, task2(2)))
	unsorted := task2(2)
	unsorted["labels"] = []any{"wayfinder:task", "implementation:task"}
	wrongURL := task2(2)
	wrongURL["url"] = "https://example.com/2"
	missing := task2(2)
	delete(missing, "bodySha256")
	cases := []struct {
		want  string
		value any
	}{
		{"not canonical", string(compact)},
		{"#2 labels must be sorted unique strings", doc(nil, unsorted)},
		{"#2 url must be the issue's GitHub URL", doc(nil, wrongURL)},
		{"issues[1] keys must be exactly", doc(nil, missing)},
		{"issues[2] numbers must be positive, unique and ascending", doc(nil, task2(3), task2(2))},
		{"is not JSON", "{"},
	}
	h := newHarness(t)
	for _, test := range cases {
		t.Run(test.want, func(t *testing.T) {
			expectFailure(t, h.offlineCheck(test.value), test.want)
		})
	}
}

// Show and errors -------------------------------------------------------------------------------------------

func TestShowLiveIncludesRelationshipsBodyAndComments(t *testing.T) {
	h := newHarness(t)
	h.serve(fixture(), map[string]any{"8": []any{map[string]any{
		"author": map[string]any{"login": "ana"}, "createdAt": "2026-10-09T01:00:00Z",
		"url": "https://github.com/acme/widgets/issues/8#c1", "body": "Resolved: ship it.",
	}}})
	out := h.live("show", "8")
	if out.code != 0 {
		t.Fatal(out.stderr)
	}
	expectContains(t, out.stdout, "status: rollup")
	expectContains(t, out.stdout, "children: #9")
	expectContains(t, out.stdout, "Resolved: ship it.")
}

func TestShowOfflineReportsRelationshipsWithoutNetwork(t *testing.T) {
	h := newHarness(t)
	path := filepath.Join(h.tmp, "snapshot.json")
	h.live("snapshot", "--output", path)
	var detail map[string]any
	if err := json.Unmarshal([]byte(h.offline("show", "2", "--json", "--snapshot", path).stdout), &detail); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%v %v %v", detail["blocking"], detail["children"], detail["status"]); got != "[4] [] ready" {
		t.Fatalf("got %v %v %v", detail["blocking"], detail["children"], detail["status"])
	}
	if _, ok := detail["comments"]; ok {
		t.Fatal("offline show must not carry comments")
	}
}

func TestLiveReadWithoutGHPointsAtTheSnapshot(t *testing.T) {
	expectFailure(t, newHarness(t).offline("frontier"), "gh is not on PATH; use --snapshot")
}

func TestLiveMapMustBeUnique(t *testing.T) {
	h := newHarness(t)
	nodes := fixture()
	nodes[2]["labels"] = conn("name", "wayfinder:map")
	h.serve(nodes, nil)
	expectFailure(t, h.live("frontier"), "expected exactly one open wayfinder:map issue, found [1, 3]")
}
