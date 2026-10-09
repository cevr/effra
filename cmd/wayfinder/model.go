package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const (
	defaultRepo = "cevr/effra"
	schemaID    = "effra-wayfinder-snapshot/1"
	mapLabel    = "wayfinder:map"
)

// typeLabels are the four ticket types; every ticket carries exactly one.
var typeLabels = []string{"wayfinder:grilling", "wayfinder:prototype", "wayfinder:research", "wayfinder:task"}

// hitlLabels mark human-in-the-loop tickets: only owner feedback resolves them, so agents never pick them up
// or close them.
var hitlLabels = []string{"wayfinder:grilling", "wayfinder:prototype", "wayfinder:research"}

const issueFields = `number title state stateReason url updatedAt body
  labels(first: 50) { totalCount nodes { name } }
  assignees(first: 20) { totalCount nodes { login } }
  parent { number repository { nameWithOwner } }
  blockedBy(first: 100) { totalCount nodes { number repository { nameWithOwner } } }`

var issuesQuery = `query($owner: String!, $name: String!, $cursor: String) {
  repository(owner: $owner, name: $name) {
    issues(first: 50, after: $cursor, orderBy: {field: CREATED_AT, direction: ASC}) {
      pageInfo { hasNextPage endCursor }
      nodes { ` + issueFields + ` }
    }
  }
}`

const commentsQuery = `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    issue(number: $number) {
      body
      comments(first: 100) { totalCount nodes { author { login } createdAt url body } }
    }
  }
}`

// projectOrder is the key order of a live issue record; snapshot records use sorted keys.
var projectOrder = []string{"number", "title", "state", "stateReason", "labels", "assignees", "parent", "blockedBy", "url", "updatedAt", "bodySha256"}

var (
	issueKeys    = sortedCopy(projectOrder)
	documentKeys = []string{"issues", "map", "mapReferences", "repository", "schema"}
	sha256Text   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	timestamp    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`)
)

type wayfinderError struct{ message string }

func (e wayfinderError) Error() string { return e.message }

func failf(format string, args ...any) error { return wayfinderError{fmt.Sprintf(format, args...)} }

// Issue is one snapshot record.
type Issue struct {
	Number      int
	Title       string
	State       string
	StateReason *string
	Labels      []string
	Assignees   []string
	Parent      *int
	BlockedBy   []int
	URL         string
	UpdatedAt   string
	BodySHA256  string
	// Foreign holds relationship endpoints in another repository, such as "is blocked by other/repo#6".
	// The map holds one repository, so `check` rejects them and a snapshot never contains them; they are never
	// reduced to bare numbers, which would alias this repository's issues.
	Foreign        []string
	foreignBlocker bool
}

// Document is the map: the snapshot schema, whether read live or from the committed mirror.
type Document struct {
	Repository    string
	Map           int
	MapReferences []int
	Issues        []Issue
	// live keeps the source's record key order for --json output: GitHub reads use projectOrder, snapshot
	// reads the file's sorted order.
	live bool
}

func (issue Issue) record(sorted bool) object {
	values := map[string]any{
		"number": issue.Number, "title": issue.Title, "state": issue.State, "stateReason": optionalString(issue.StateReason),
		"labels": stringValues(issue.Labels), "assignees": stringValues(issue.Assignees), "parent": optionalInt(issue.Parent),
		"blockedBy": intValues(issue.BlockedBy), "url": issue.URL, "updatedAt": issue.UpdatedAt, "bodySha256": issue.BodySHA256,
	}
	order := projectOrder
	if sorted {
		order = issueKeys
	}
	fields := make(object, 0, len(order))
	for _, key := range order {
		fields = append(fields, field{key, values[key]})
	}
	return fields
}

func (document Document) value() object {
	issues := make([]any, len(document.Issues))
	for index, issue := range document.Issues {
		issues[index] = issue.record(true)
	}
	return object{
		{"schema", schemaID}, {"repository", document.Repository}, {"map", document.Map},
		{"mapReferences", intValues(document.MapReferences)}, {"issues", issues},
	}
}

func render(document Document) string { return renderJSON(document.value(), true) + "\n" }

// Live source -----------------------------------------------------------------------------------------------

type variable struct {
	key   string
	value any // string, int or nil (omitted)
}

func graphql(query string, variables []variable, target any) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return failf("gh is not on PATH; use --snapshot for offline reads")
	}
	args := []string{"api", "graphql", "-f", "query=" + query}
	for _, item := range variables {
		switch value := item.value.(type) {
		case nil:
		case int:
			args = append(args, "-F", fmt.Sprintf("%s=%d", item.key, value))
		default:
			args = append(args, "-f", fmt.Sprintf("%s=%v", item.key, value))
		}
	}
	var stdout, stderr bytes.Buffer
	command := exec.Command("gh", args...)
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return failf("gh api graphql failed: %v", err)
		}
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		return failf("gh api graphql failed: %s", detail)
	}
	var payload struct {
		Data   json.RawMessage `json:"data"`
		Errors []any           `json:"errors"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		return failf("gh api graphql returned invalid JSON: %v", err)
	}
	if len(payload.Errors) > 0 {
		encoded, _ := json.Marshal(payload.Errors)
		return failf("GitHub GraphQL errors: %s", encoded)
	}
	if err := json.Unmarshal(payload.Data, target); err != nil {
		return failf("gh api graphql returned unexpected data: %v", err)
	}
	return nil
}

func splitRepo(repo string) (string, string, error) {
	owner, name, _ := strings.Cut(repo, "/")
	if owner == "" || name == "" || strings.Contains(name, "/") {
		return "", "", failf("repository must be OWNER/NAME, not '%s'", repo)
	}
	return owner, name, nil
}

type connection[T any] struct {
	TotalCount int `json:"totalCount"`
	Nodes      []T `json:"nodes"`
}

type gqlIssue struct {
	Number      int     `json:"number"`
	Title       string  `json:"title"`
	State       string  `json:"state"`
	StateReason *string `json:"stateReason"`
	URL         string  `json:"url"`
	UpdatedAt   string  `json:"updatedAt"`
	Body        *string `json:"body"`
	Labels      connection[struct {
		Name string `json:"name"`
	}] `json:"labels"`
	Assignees connection[struct {
		Login string `json:"login"`
	}] `json:"assignees"`
	Parent    *endpoint            `json:"parent"`
	BlockedBy connection[endpoint] `json:"blockedBy"`
}

// endpoint is a relationship's other issue, which GitHub allows to live in another repository.
type endpoint struct {
	Number     int `json:"number"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

// local reports whether the endpoint is in repo. GitHub owner and repository names are case-insensitive.
func (e endpoint) local(repo string) bool { return strings.EqualFold(e.Repository.NameWithOwner, repo) }

func (e endpoint) String() string { return fmt.Sprintf("%s#%d", e.Repository.NameWithOwner, e.Number) }

func checkPage(node gqlIssue, field string, total, length int) error {
	if total > length {
		return failf("#%d has more %s than one page; extend the query", node.Number, field)
	}
	return nil
}

func (node gqlIssue) labels() ([]string, error) {
	if err := checkPage(node, "labels", node.Labels.TotalCount, len(node.Labels.Nodes)); err != nil {
		return nil, err
	}
	values := make([]string, len(node.Labels.Nodes))
	for index, item := range node.Labels.Nodes {
		values[index] = item.Name
	}
	return values, nil
}

func fetchIssues(repo string) ([]gqlIssue, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}
	var nodes []gqlIssue
	var cursor any
	for {
		var data struct {
			Repository struct {
				Issues struct {
					PageInfo struct {
						HasNextPage bool   `json:"hasNextPage"`
						EndCursor   string `json:"endCursor"`
					} `json:"pageInfo"`
					Nodes []gqlIssue `json:"nodes"`
				} `json:"issues"`
			} `json:"repository"`
		}
		if err := graphql(issuesQuery, []variable{{"owner", owner}, {"name", name}, {"cursor", cursor}}, &data); err != nil {
			return nil, err
		}
		page := data.Repository.Issues
		nodes = append(nodes, page.Nodes...)
		if !page.PageInfo.HasNextPage {
			return nodes, nil
		}
		cursor = page.PageInfo.EndCursor
	}
}

// project maps one GraphQL issue node to its snapshot record.
func project(node gqlIssue, repo string) (Issue, error) {
	labels, err := node.labels()
	if err != nil {
		return Issue{}, err
	}
	if err := checkPage(node, "assignees", node.Assignees.TotalCount, len(node.Assignees.Nodes)); err != nil {
		return Issue{}, err
	}
	assignees := make([]string, len(node.Assignees.Nodes))
	for index, item := range node.Assignees.Nodes {
		assignees[index] = item.Login
	}
	if err := checkPage(node, "blockedBy", node.BlockedBy.TotalCount, len(node.BlockedBy.Nodes)); err != nil {
		return Issue{}, err
	}
	var blockers []int
	var foreign []string
	foreignBlocker := false
	for _, item := range node.BlockedBy.Nodes {
		if item.local(repo) {
			blockers = append(blockers, item.Number)
		} else {
			foreign = append(foreign, "is blocked by "+item.String())
			foreignBlocker = true
		}
	}
	issue := Issue{
		Number: node.Number, Title: node.Title, State: strings.ToLower(node.State), Labels: uniqueStrings(labels),
		Assignees: uniqueStrings(assignees), BlockedBy: uniqueInts(blockers), URL: node.URL, UpdatedAt: node.UpdatedAt,
		foreignBlocker: foreignBlocker,
	}
	if node.StateReason != nil && *node.StateReason != "" {
		reason := strings.ToLower(*node.StateReason)
		issue.StateReason = &reason
	}
	if node.Parent != nil && node.Parent.local(repo) {
		parent := node.Parent.Number
		issue.Parent = &parent
	} else if node.Parent != nil {
		foreign = append(foreign, "has parent "+node.Parent.String())
	}
	issue.Foreign = foreign
	body := ""
	if node.Body != nil {
		body = *node.Body
	}
	digest := sha256.Sum256([]byte(body))
	issue.BodySHA256 = hex.EncodeToString(digest[:])
	return issue, nil
}

// issueReferences finds `#N` (not preceded by a word character, `/` or `&`) and full issue URLs.
func issueReferences(body, repo string) []int {
	pattern := regexp.MustCompile(`(github\.com/` + regexp.QuoteMeta(repo) + `/issues/|#)([0-9]+)`)
	found := map[int]bool{}
	for _, match := range pattern.FindAllStringSubmatchIndex(body, -1) {
		if body[match[2]:match[3]] == "#" && match[0] > 0 {
			previous := lastRune(body[:match[0]])
			if isWord(previous) || previous == '/' || previous == '&' {
				continue
			}
		}
		if match[1] < len(body) && isWord(firstRune(body[match[1]:])) {
			continue
		}
		number, err := strconv.Atoi(body[match[4]:match[5]])
		if err == nil {
			found[number] = true
		}
	}
	return sortedKeys(found)
}

// buildMap selects the map tree from every repository issue and reports open Wayfinder issues outside it.
func buildMap(nodes []gqlIssue, repo string) (Document, []string, error) {
	var maps []gqlIssue
	for _, node := range nodes {
		if node.State != "OPEN" {
			continue
		}
		labels, err := node.labels()
		if err != nil {
			return Document{}, nil, err
		}
		if contains(labels, mapLabel) {
			maps = append(maps, node)
		}
	}
	if len(maps) != 1 {
		numbers := make([]int, len(maps))
		for index, node := range maps {
			numbers[index] = node.Number
		}
		return Document{}, nil, failf("expected exactly one open %s issue, found %s", mapLabel, intList(numbers))
	}
	root := maps[0]
	children := map[int][]gqlIssue{}
	for _, node := range nodes {
		// A parent in another repository is not this map's issue, whatever its number.
		if node.Parent != nil && node.Parent.local(repo) {
			children[node.Parent.Number] = append(children[node.Parent.Number], node)
		}
	}
	tree, pending := []gqlIssue{root}, []int{root.Number}
	for len(pending) > 0 {
		next := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		for _, child := range children[next] {
			tree = append(tree, child)
			pending = append(pending, child.Number)
		}
	}
	members := map[int]bool{}
	for _, node := range tree {
		members[node.Number] = true
	}
	var strays []string
	for _, node := range nodes {
		if members[node.Number] || node.State != "OPEN" {
			continue
		}
		labels, err := node.labels()
		if err != nil {
			return Document{}, nil, err
		}
		for _, label := range labels {
			if strings.HasPrefix(label, "wayfinder:") {
				strays = append(strays, fmt.Sprintf("#%d is open with Wayfinder labels but is not under the map", node.Number))
				break
			}
		}
	}
	body := ""
	if root.Body != nil {
		body = *root.Body
	}
	document := Document{Repository: repo, Map: root.Number, MapReferences: issueReferences(body, repo), live: true}
	for _, node := range tree {
		issue, err := project(node, repo)
		if err != nil {
			return Document{}, nil, err
		}
		document.Issues = append(document.Issues, issue)
	}
	sort.Slice(document.Issues, func(i, j int) bool { return document.Issues[i].Number < document.Issues[j].Number })
	return document, strays, nil
}

func loadLive(repo string) (Document, []string, error) {
	nodes, err := fetchIssues(repo)
	if err != nil {
		return Document{}, nil, err
	}
	return buildMap(nodes, repo)
}

// Snapshot source -------------------------------------------------------------------------------------------

func loadSnapshot(path string) (Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		reason := err.Error()
		var pathError *fs.PathError
		if errors.As(err, &pathError) {
			reason = capitalize(pathError.Err.Error())
		}
		return Document{}, failf("cannot read snapshot %s: %s", path, reason)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var parsed any
	if err := decoder.Decode(&parsed); err != nil {
		return Document{}, failf("snapshot %s is not JSON: %v", path, err)
	}
	if decoder.More() {
		return Document{}, failf("snapshot %s is not JSON: extra data after the document", path)
	}
	problems := schemaErrors(parsed)
	if len(problems) == 0 && renderJSON(parsed, true)+"\n" != string(data) {
		problems = append(problems, "snapshot is not canonical; regenerate it with `go run ./cmd/wayfinder snapshot`")
	}
	if len(problems) > 0 {
		return Document{}, failf("invalid snapshot %s:\n  %s", path, strings.Join(problems, "\n  "))
	}
	return decodeDocument(parsed.(map[string]any)), nil
}

func schemaErrors(parsed any) []string {
	document, ok := parsed.(map[string]any)
	if !ok || !sameKeys(document, documentKeys) {
		return []string{"snapshot keys must be exactly " + pyList(documentKeys)}
	}
	var problems []string
	if document["schema"] != schemaID {
		problems = append(problems, fmt.Sprintf("schema must be '%s'", schemaID))
	}
	repo, isString := document["repository"].(string)
	if !isString || strings.Count(repo, "/") != 1 {
		problems = append(problems, "repository must be OWNER/NAME")
	}
	if _, ok := asInt(document["map"]); !ok {
		problems = append(problems, "map must be an issue number")
	}
	if !isSortedInts(document["mapReferences"]) {
		problems = append(problems, "mapReferences must be sorted unique issue numbers")
	}
	issues, ok := document["issues"].([]any)
	if !ok || len(issues) == 0 {
		return append(problems, "issues must be a non-empty list")
	}
	previous := 0
	for position, item := range issues {
		issue, ok := item.(map[string]any)
		if !ok || !sameKeys(issue, issueKeys) {
			problems = append(problems, fmt.Sprintf("issues[%d] keys must be exactly %s", position, pyList(issueKeys)))
			continue
		}
		number, ok := asInt(issue["number"])
		if !ok || number <= previous {
			problems = append(problems, fmt.Sprintf("issues[%d] numbers must be positive, unique and ascending", position))
			continue
		}
		previous = number
		if title, ok := issue["title"].(string); !ok || title == "" {
			problems = append(problems, fmt.Sprintf("#%d title must be a non-empty string", number))
		}
		if state := issue["state"]; state != "open" && state != "closed" {
			problems = append(problems, fmt.Sprintf("#%d state must be open or closed", number))
		}
		if reason := issue["stateReason"]; reason != nil {
			if text, ok := reason.(string); !ok || !isLowercase(text) {
				problems = append(problems, fmt.Sprintf("#%d stateReason must be null or a lowercase reason", number))
			}
		}
		for _, key := range []string{"labels", "assignees"} {
			if !isSortedStrings(issue[key]) {
				problems = append(problems, fmt.Sprintf("#%d %s must be sorted unique strings", number, key))
			}
		}
		if parent := issue["parent"]; parent != nil {
			if _, ok := asInt(parent); !ok {
				problems = append(problems, fmt.Sprintf("#%d parent must be null or an issue number", number))
			}
		}
		if !isSortedInts(issue["blockedBy"]) {
			problems = append(problems, fmt.Sprintf("#%d blockedBy must be sorted unique issue numbers", number))
		}
		if issue["url"] != fmt.Sprintf("https://github.com/%s/issues/%d", pyStr(document["repository"]), number) {
			problems = append(problems, fmt.Sprintf("#%d url must be the issue's GitHub URL", number))
		}
		if text, ok := issue["updatedAt"].(string); !ok || !timestamp.MatchString(text) {
			problems = append(problems, fmt.Sprintf("#%d updatedAt must be an ISO-8601 UTC timestamp", number))
		}
		if text, ok := issue["bodySha256"].(string); !ok || !sha256Text.MatchString(text) {
			problems = append(problems, fmt.Sprintf("#%d bodySha256 must be a lowercase SHA-256 digest", number))
		}
	}
	return problems
}

// decodeDocument converts a schema-valid parsed snapshot.
func decodeDocument(parsed map[string]any) Document {
	document := Document{Repository: parsed["repository"].(string), MapReferences: ints(parsed["mapReferences"])}
	document.Map, _ = asInt(parsed["map"])
	for _, item := range parsed["issues"].([]any) {
		record := item.(map[string]any)
		issue := Issue{
			Title: record["title"].(string), State: record["state"].(string), Labels: strs(record["labels"]),
			Assignees: strs(record["assignees"]), BlockedBy: ints(record["blockedBy"]), URL: record["url"].(string),
			UpdatedAt: record["updatedAt"].(string), BodySHA256: record["bodySha256"].(string),
		}
		issue.Number, _ = asInt(record["number"])
		if reason, ok := record["stateReason"].(string); ok {
			issue.StateReason = &reason
		}
		if parent, ok := asInt(record["parent"]); ok {
			issue.Parent = &parent
		}
		document.Issues = append(document.Issues, issue)
	}
	return document
}

// Derived views ---------------------------------------------------------------------------------------------

func (document Document) index() map[int]*Issue {
	result := make(map[int]*Issue, len(document.Issues))
	for position := range document.Issues {
		result[document.Issues[position].Number] = &document.Issues[position]
	}
	return result
}

func (document Document) openChildren() map[int][]int {
	result := map[int][]int{}
	for _, issue := range document.Issues {
		if issue.Parent != nil && issue.State == "open" {
			result[*issue.Parent] = append(result[*issue.Parent], issue.Number)
		}
	}
	return result
}

// openBlockers treats a blocker outside the map as open: `check` reports it, and work must not start on a guess.
func openBlockers(issue Issue, issues map[int]*Issue) []int {
	var result []int
	for _, number := range issue.BlockedBy {
		if blocker, ok := issues[number]; !ok || blocker.State != "closed" {
			result = append(result, number)
		}
	}
	return result
}

func kind(issue Issue) string {
	var kinds []string
	for _, label := range issue.Labels {
		if contains(typeLabels, label) || label == mapLabel {
			kinds = append(kinds, strings.TrimPrefix(label, "wayfinder:"))
		}
	}
	if len(kinds) == 0 {
		return "untyped"
	}
	return strings.Join(kinds, ",")
}

// status derives one of map, closed, claimed, blocked, rollup, hitl or ready (the frontier).
func (document Document) status(issue Issue) string {
	switch {
	case issue.Number == document.Map:
		return "map"
	case issue.State == "closed":
		return "closed"
	case len(issue.Assignees) > 0:
		return "claimed"
	case issue.foreignBlocker || len(openBlockers(issue, document.index())) > 0:
		return "blocked"
	case len(document.openChildren()[issue.Number]) > 0:
		return "rollup"
	}
	for _, label := range issue.Labels {
		if contains(hitlLabels, label) {
			return "hitl"
		}
	}
	return "ready"
}

func (document Document) frontier(includeHITL bool) []Issue {
	var result []Issue
	for _, issue := range document.Issues {
		status := document.status(issue)
		if status == "ready" || (includeHITL && status == "hitl") {
			result = append(result, issue)
		}
	}
	return result
}

func findCycle(edges map[int][]int) []int {
	state := map[int]int{}
	var stack []int
	var visit func(node int) []int
	visit = func(node int) []int {
		state[node] = 1
		stack = append(stack, node)
		for _, target := range edges[node] {
			if state[target] == 1 {
				for position, item := range stack {
					if item == target {
						return append(append([]int(nil), stack[position:]...), target)
					}
				}
			}
			if _, seen := state[target]; !seen {
				if cycle := visit(target); cycle != nil {
					return cycle
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[node] = 2
		return nil
	}
	nodes := make([]int, 0, len(edges))
	for node := range edges {
		nodes = append(nodes, node)
	}
	sort.Ints(nodes)
	for _, node := range nodes {
		if _, seen := state[node]; !seen {
			if cycle := visit(node); cycle != nil {
				return cycle
			}
		}
	}
	return nil
}

// check returns the map's structural errors and warnings.
func (document Document) check() ([]string, []string) {
	issues := document.index()
	var errs, warnings []string
	var maps []int
	for _, issue := range document.Issues {
		if contains(issue.Labels, mapLabel) {
			maps = append(maps, issue.Number)
		}
	}
	if len(maps) != 1 || maps[0] != document.Map {
		found := refs(maps)
		if found == "" {
			found = "none"
		}
		errs = append(errs, fmt.Sprintf("exactly #%d must carry %s, found %s", document.Map, mapLabel, found))
	}
	root, ok := issues[document.Map]
	if !ok {
		return append(errs, fmt.Sprintf("map #%d is not in the issue list", document.Map)), warnings
	}
	if root.Parent != nil || root.State != "open" {
		errs = append(errs, fmt.Sprintf("map #%d must be open and have no parent", root.Number))
	}
	for _, issue := range document.Issues {
		// Relationship checks apply to every issue, the map included.
		for _, foreign := range issue.Foreign {
			errs = append(errs, fmt.Sprintf("#%d %s, outside %s; the map holds one repository", issue.Number, foreign, document.Repository))
		}
		for _, blocker := range issue.BlockedBy {
			if _, ok := issues[blocker]; !ok {
				errs = append(errs, fmt.Sprintf("#%d is blocked by #%d, which is not in the map", issue.Number, blocker))
			}
		}
		if issue.State == "closed" {
			if stale := openBlockers(issue, issues); len(stale) > 0 {
				warnings = append(warnings, fmt.Sprintf("#%d is closed but still blocked by open %s; the edge looks stale", issue.Number, refs(stale)))
			}
		}
		// Ticket checks: the map has no parent and no type label.
		if issue.Number == root.Number {
			continue
		}
		var parent *Issue
		if issue.Parent != nil {
			parent = issues[*issue.Parent]
		}
		switch {
		case parent == nil:
			named := "(none)"
			if issue.Parent != nil && *issue.Parent != 0 {
				named = refs([]int{*issue.Parent})
			}
			errs = append(errs, fmt.Sprintf("#%d parent %s is not in the map", issue.Number, named))
		case issue.State == "open" && parent.State == "closed":
			errs = append(errs, fmt.Sprintf("#%d is open under closed parent #%d", issue.Number, parent.Number))
		}
		var types []string
		for _, label := range typeLabels {
			if contains(issue.Labels, label) {
				types = append(types, label)
			}
		}
		if len(types) != 1 {
			errs = append(errs, fmt.Sprintf("#%d needs exactly one type label from %s, has %s", issue.Number, pyList(typeLabels), pyList(types)))
		}
	}
	for _, reference := range document.MapReferences {
		if _, ok := issues[reference]; !ok {
			errs = append(errs, fmt.Sprintf("map body references #%d, which is not in the map", reference))
		}
	}
	blockedBy, parents := map[int][]int{}, map[int][]int{}
	for _, issue := range document.Issues {
		blockedBy[issue.Number] = issue.BlockedBy
		if issue.Parent != nil {
			if _, ok := issues[*issue.Parent]; ok {
				parents[issue.Number] = []int{*issue.Parent}
			}
		}
	}
	if cycle := findCycle(blockedBy); cycle != nil {
		errs = append(errs, "blocked-by cycle: "+arrows(cycle))
	}
	if cycle := findCycle(parents); cycle != nil {
		errs = append(errs, "parent cycle: "+arrows(cycle))
	}
	return errs, warnings
}

// Small helpers ---------------------------------------------------------------------------------------------

func refs(numbers []int) string {
	text := make([]string, len(numbers))
	for index, number := range numbers {
		text[index] = "#" + strconv.Itoa(number)
	}
	return strings.Join(text, ",")
}

func arrows(numbers []int) string {
	text := make([]string, len(numbers))
	for index, number := range numbers {
		text[index] = "#" + strconv.Itoa(number)
	}
	return strings.Join(text, " -> ")
}

func contains(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func sortedCopy(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	result := []string{}
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func uniqueInts(values []int) []int {
	seen := map[int]bool{}
	for _, value := range values {
		seen[value] = true
	}
	return sortedKeys(seen)
}

func sortedKeys(set map[int]bool) []int {
	result := make([]int, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}

func sameKeys(values map[string]any, keys []string) bool {
	if len(values) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := values[key]; !ok {
			return false
		}
	}
	return true
}

// asInt accepts a JSON integer (not a float, string or boolean).
func asInt(value any) (int, bool) {
	number, ok := value.(json.Number)
	if !ok || !integerText.MatchString(string(number)) {
		return 0, false
	}
	parsed, err := strconv.Atoi(string(number))
	return parsed, err == nil
}

func isSortedInts(value any) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for index, item := range items {
		number, ok := asInt(item)
		if !ok {
			return false
		}
		if index > 0 {
			if previous, _ := asInt(items[index-1]); number <= previous {
				return false
			}
		}
	}
	return true
}

func isSortedStrings(value any) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for index, item := range items {
		text, ok := item.(string)
		if !ok {
			return false
		}
		if index > 0 {
			if previous, _ := items[index-1].(string); text <= previous {
				return false
			}
		}
	}
	return true
}

// isLowercase matches Python's str.islower: at least one cased character and no upper- or title-case one.
func isLowercase(text string) bool {
	cased := false
	for _, r := range text {
		if unicode.IsUpper(r) || unicode.IsTitle(r) {
			return false
		}
		if unicode.IsLower(r) {
			cased = true
		}
	}
	return cased
}

func pyStr(value any) string {
	switch v := value.(type) {
	case nil:
		return "None"
	case bool:
		if v {
			return "True"
		}
		return "False"
	case string:
		return v
	case json.Number:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}

func ints(value any) []int {
	result := []int{}
	for _, item := range value.([]any) {
		number, _ := asInt(item)
		result = append(result, number)
	}
	return result
}

func strs(value any) []string {
	result := []string{}
	for _, item := range value.([]any) {
		result = append(result, item.(string))
	}
	return result
}

func stringValues(values []string) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}

func intValues(values []int) []any {
	result := make([]any, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}

func optionalString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func optionalInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func isWord(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsNumber(r)
}

func firstRune(text string) rune {
	for _, r := range text {
		return r
	}
	return 0
}

func lastRune(text string) rune {
	runes := []rune(text)
	return runes[len(runes)-1]
}

func capitalize(text string) string {
	if text == "" {
		return text
	}
	runes := []rune(text)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
