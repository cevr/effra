// Command wayfinder reads the Wayfinder map. GitHub is canonical; docs/wayfinder/snapshot.json is a derived
// offline mirror.
//
// The map is the open issue labelled `wayfinder:map` plus its transitive native sub-issues. Dependencies are
// native `blocked by` relationships. Live reads go through `gh api graphql`; `--snapshot` reads the committed
// mirror without network access. This command never writes to GitHub.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

const snapshotRelative = "docs/wayfinder/snapshot.json"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// Command line ----------------------------------------------------------------------------------------------

type options struct {
	command     string
	repo        string
	snapshot    *string // nil: read GitHub
	json        bool
	all         bool
	number      int
	output      string
	checkOutput bool
}

type usageError struct{ command, message string }

func (e usageError) Error() string { return e.message }

type helpRequest struct{ command string }

func (helpRequest) Error() string { return "help" }

var commandNames = []string{"frontier", "list", "show", "check", "snapshot"}

// commandOptions lists each command's long options; true marks an option that takes a value.
var commandOptions = map[string]map[string]bool{
	"frontier": {"--repo": true, "--snapshot": false, "--json": false, "--all": false},
	"list":     {"--repo": true, "--snapshot": false, "--json": false},
	"show":     {"--repo": true, "--snapshot": false, "--json": false},
	"check":    {"--repo": true, "--snapshot": false},
	"snapshot": {"--repo": true, "--snapshot": false, "--output": true, "--check": false},
}

var commandHelp = map[string]string{
	"frontier": "open, unclaimed, unblocked leaf tickets (--all adds HITL tickets)",
	"list":     "every ticket under the map with its derived status",
	"show":     "one ticket; live reads include body and comments",
	"check":    "structural check: references, cycles, labels, parent state",
	"snapshot": "write the derived snapshot from GitHub (--check: exit 1 if it differs; write nothing)",
}

func isNegativeNumber(token string) bool {
	if len(token) < 2 || token[0] != '-' {
		return false
	}
	_, err := strconv.ParseFloat(token[1:], 64)
	return err == nil && token[1] != '-'
}

func isOption(token string) bool {
	return strings.HasPrefix(token, "-") && token != "-" && !isNegativeNumber(token)
}

// resolveOption matches an exact option or a unique prefix, as argparse does.
func resolveOption(command, name string) (string, error) {
	known := commandOptions[command]
	if name == "-h" || name == "--help" {
		return "--help", nil
	}
	if _, ok := known[name]; ok {
		return name, nil
	}
	var matches []string
	for option := range known {
		if strings.HasPrefix(option, name) && strings.HasPrefix(name, "--") {
			matches = append(matches, option)
		}
	}
	if strings.HasPrefix("--help", name) && strings.HasPrefix(name, "--") {
		matches = append(matches, "--help")
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", usageError{"", "unrecognized arguments: " + name}
	default:
		return "", usageError{command, fmt.Sprintf("ambiguous option: %s could match %s", name, strings.Join(sortedCopy(matches), ", "))}
	}
}

func parseArgs(args []string, defaultSnapshot string) (options, error) {
	parsed := options{repo: defaultRepo, output: defaultSnapshot}
	if len(args) == 0 {
		return parsed, usageError{"", "the following arguments are required: command"}
	}
	if args[0] == "-h" || args[0] == "--help" {
		return parsed, helpRequest{}
	}
	if isOption(args[0]) {
		return parsed, usageError{"", "the following arguments are required: command"}
	}
	parsed.command = args[0]
	if _, ok := commandOptions[parsed.command]; !ok {
		quoted := make([]string, len(commandNames))
		for index, name := range commandNames {
			quoted[index] = "'" + name + "'"
		}
		return parsed, usageError{"", fmt.Sprintf("argument command: invalid choice: '%s' (choose from %s)", parsed.command, strings.Join(quoted, ", "))}
	}
	var positionals []string
	rest := args[1:]
	for index := 0; index < len(rest); index++ {
		token := rest[index]
		if token == "--" {
			positionals = append(positionals, rest[index+1:]...)
			break
		}
		if !isOption(token) {
			positionals = append(positionals, token)
			continue
		}
		name, inline, hasInline := strings.Cut(token, "=")
		option, err := resolveOption(parsed.command, name)
		if err != nil {
			return parsed, err
		}
		if option == "--help" {
			return parsed, helpRequest{parsed.command}
		}
		takesValue := commandOptions[parsed.command][option]
		value := ""
		switch {
		case takesValue && hasInline:
			value = inline
		case takesValue:
			if index+1 >= len(rest) || isOption(rest[index+1]) {
				return parsed, usageError{parsed.command, fmt.Sprintf("argument %s: expected one argument", option)}
			}
			index++
			value = rest[index]
		case option == "--snapshot":
			// nargs="?": an inline value or a following non-option token is the path; otherwise the default.
			value = defaultSnapshot
			if hasInline {
				value = inline
			} else if index+1 < len(rest) && !isOption(rest[index+1]) {
				index++
				value = rest[index]
			}
		case hasInline:
			return parsed, usageError{parsed.command, fmt.Sprintf("argument %s: ignored explicit argument '%s'", option, inline)}
		}
		switch option {
		case "--repo":
			parsed.repo = value
		case "--snapshot":
			path := value
			parsed.snapshot = &path
		case "--output":
			parsed.output = value
		case "--json":
			parsed.json = true
		case "--all":
			parsed.all = true
		case "--check":
			parsed.checkOutput = true
		}
	}
	if parsed.command == "show" {
		if len(positionals) == 0 {
			return parsed, usageError{parsed.command, "the following arguments are required: number"}
		}
		number, err := strconv.Atoi(strings.TrimSpace(positionals[0]))
		if err != nil {
			return parsed, usageError{parsed.command, fmt.Sprintf("argument number: invalid int value: '%s'", positionals[0])}
		}
		parsed.number = number
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return parsed, usageError{"", "unrecognized arguments: " + strings.Join(positionals, " ")}
	}
	return parsed, nil
}

func usage(command string) string {
	if command == "" {
		return "usage: wayfinder [-h] {" + strings.Join(commandNames, ",") + "} ..."
	}
	flags := map[string]string{
		"frontier": "[-h] [--repo REPO] [--snapshot [PATH]] [--json] [--all]",
		"list":     "[-h] [--repo REPO] [--snapshot [PATH]] [--json]",
		"show":     "[-h] [--repo REPO] [--snapshot [PATH]] [--json] number",
		"check":    "[-h] [--repo REPO] [--snapshot [PATH]]",
		"snapshot": "[-h] [--repo REPO] [--snapshot [PATH]] [--output OUTPUT] [--check]",
	}
	return "usage: wayfinder " + command + " " + flags[command]
}

func help(command string) string {
	var out strings.Builder
	out.WriteString(usage(command) + "\n\n")
	if command == "" {
		out.WriteString("Read the Wayfinder map. GitHub is canonical; docs/wayfinder/snapshot.json is a derived offline mirror.\n\ncommands:\n")
		for _, name := range commandNames {
			fmt.Fprintf(&out, "  %-9s %s\n", name, commandHelp[name])
		}
		return out.String()
	}
	out.WriteString(commandHelp[command] + "\n\noptions:\n")
	out.WriteString("  --repo REPO        GitHub repository (default " + defaultRepo + ")\n")
	out.WriteString("  --snapshot [PATH]  read the committed snapshot instead of GitHub (default path " + snapshotRelative + ")\n")
	if command != "check" && command != "snapshot" {
		out.WriteString("  --json             print JSON\n")
	}
	switch command {
	case "frontier":
		out.WriteString("  --all              include HITL tickets (grilling/prototype/research)\n")
	case "snapshot":
		out.WriteString("  --output OUTPUT    snapshot path (default " + snapshotRelative + ")\n")
		out.WriteString("  --check            exit 1 if the file differs from live GitHub; write nothing\n")
	}
	return out.String()
}

// Repository paths ------------------------------------------------------------------------------------------

// repositoryRoot is the nearest ancestor of the working directory holding go.mod, or "" outside the repository.
func repositoryRoot() string {
	directory, err := os.Getwd()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(directory); err == nil {
		directory = resolved
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return ""
		}
		directory = parent
	}
}

func resolvePath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		return resolved
	}
	return absolute
}

func display(root, path string) string {
	if root == "" {
		return path
	}
	relative, err := filepath.Rel(root, resolvePath(path))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return path
	}
	return relative
}

// Commands --------------------------------------------------------------------------------------------------

type cli struct {
	options
	root           string
	stdout, stderr io.Writer
}

func run(args []string, stdout, stderr io.Writer) int {
	root := repositoryRoot()
	defaultSnapshot := snapshotRelative
	if root != "" {
		defaultSnapshot = filepath.Join(root, snapshotRelative)
	}
	parsed, err := parseArgs(args, defaultSnapshot)
	var helpWanted helpRequest
	if errors.As(err, &helpWanted) {
		fmt.Fprint(stdout, help(helpWanted.command))
		return 0
	}
	var bad usageError
	if errors.As(err, &bad) {
		fmt.Fprintln(stderr, usage(bad.command))
		name := "wayfinder"
		if bad.command != "" {
			name += " " + bad.command
		}
		fmt.Fprintf(stderr, "%s: error: %s\n", name, bad.message)
		return 2
	}
	c := cli{options: parsed, root: root, stdout: stdout, stderr: stderr}
	code, err := c.execute()
	if err != nil {
		fmt.Fprintf(stderr, "wayfinder: %v\n", err)
		return 1
	}
	return code
}

func (c cli) execute() (int, error) {
	if c.command == "snapshot" && c.snapshot != nil {
		return 1, failf("snapshot reads GitHub; it cannot take --snapshot")
	}
	var document Document
	var strays []string
	var err error
	if c.snapshot != nil {
		document, err = loadSnapshot(*c.snapshot)
	} else {
		document, strays, err = loadLive(c.repo)
	}
	if err != nil {
		return 1, err
	}
	switch c.command {
	case "frontier":
		c.emit(document, document.frontier(c.all))
	case "list":
		var issues []Issue
		for _, issue := range document.Issues {
			if issue.Number != document.Map {
				issues = append(issues, issue)
			}
		}
		c.emit(document, issues)
	case "show":
		return c.show(document)
	case "check":
		return c.runCheck(document, strays), nil
	case "snapshot":
		return c.writeSnapshot(document)
	}
	return 0, nil
}

func (c cli) line(document Document, issue Issue) string {
	status := document.status(issue)
	detail := ""
	switch status {
	case "blocked":
		detail = " (blocked by " + qualified(openBlockers(issue, document.index()), names(issue.ForeignBlockers, true)) + ")"
	case "claimed":
		logins := make([]string, len(issue.Assignees))
		for index, login := range issue.Assignees {
			logins[index] = "@" + login
		}
		detail = " (claimed by " + strings.Join(logins, ",") + ")"
	case "rollup":
		detail = " (open children " + qualified(document.openChildren()[issue.Number], names(issue.ForeignChildren, true)) + ")"
	}
	return fmt.Sprintf("#%-4d %-8s %-10s %s%s", issue.Number, status, kind(issue), issue.Title, detail)
}

// qualified lists local issues as #N and foreign ones as OWNER/NAME#N.
func qualified(local []int, foreign []string) string {
	var parts []string
	if text := refs(local); text != "" {
		parts = append(parts, text)
	}
	return strings.Join(append(parts, foreign...), ",")
}

// liveRecord is an issue's JSON record; live reads add its foreign relationships, which a snapshot never has.
func liveRecord(document Document, issue Issue) object {
	record := issue.record(!document.live)
	if document.live {
		record = append(record, field{"foreign", issue.foreignValue()})
	}
	return record
}

func (c cli) emit(document Document, issues []Issue) {
	if !c.json {
		for _, issue := range issues {
			fmt.Fprintln(c.stdout, c.line(document, issue))
		}
		return
	}
	values := make([]any, len(issues))
	for index, issue := range issues {
		values[index] = append(liveRecord(document, issue), field{"status", document.status(issue)})
	}
	fmt.Fprintln(c.stdout, renderJSON(values, false))
}

type liveComment struct {
	Author *struct {
		Login string `json:"login"`
	} `json:"author"`
	CreatedAt string `json:"createdAt"`
	URL       string `json:"url"`
	Body      string `json:"body"`
}

func (c cli) show(document Document) (int, error) {
	issues := document.index()
	issue, ok := issues[c.number]
	if !ok {
		return 1, failf("#%d is not in the map", c.number)
	}
	var children, blocking []int
	for _, item := range document.Issues {
		if item.Parent != nil && *item.Parent == c.number {
			children = append(children, item.Number)
		}
		if containsInt(item.BlockedBy, c.number) {
			blocking = append(blocking, item.Number)
		}
	}
	status := document.status(*issue)
	detail := append(liveRecord(document, *issue), field{"status", status}, field{"children", intValues(children)}, field{"blocking", intValues(blocking)})
	var body *string
	var comments []liveComment
	if c.snapshot == nil {
		owner, name, err := splitRepo(document.Repository)
		if err != nil {
			return 1, err
		}
		var data struct {
			Repository struct {
				Issue struct {
					Body     *string                 `json:"body"`
					Comments connection[liveComment] `json:"comments"`
				} `json:"issue"`
			} `json:"repository"`
		}
		if err := graphql(commentsQuery, []variable{{"owner", owner}, {"name", name}, {"number", c.number}}, &data); err != nil {
			return 1, err
		}
		live := data.Repository.Issue
		body, comments = live.Body, live.Comments.Nodes
		values := make([]any, len(comments))
		for index, comment := range comments {
			values[index] = object{{"author", authorValue(comment)}, {"createdAt", comment.CreatedAt}, {"url", comment.URL}, {"body", comment.Body}}
		}
		detail = append(detail, field{"body", optionalString(body)}, field{"comments", values})
		if live.Comments.TotalCount > len(comments) {
			return 1, failf("#%d has more comments than one page; extend the query", c.number)
		}
	}
	if c.json {
		fmt.Fprintln(c.stdout, renderJSON(detail, false))
		return 0, nil
	}
	reason := ""
	if issue.StateReason != nil {
		reason = " (" + *issue.StateReason + ")"
	}
	parent := "-"
	if issue.Parent != nil && *issue.Parent != 0 {
		parent = refs([]int{*issue.Parent})
	} else if issue.ForeignParent != nil {
		parent = issue.ForeignParent.Name
	}
	fmt.Fprintf(c.stdout, "#%d %s\n%s\n", issue.Number, issue.Title, issue.URL)
	fmt.Fprintf(c.stdout, "status: %s  state: %s%s\n", status, issue.State, reason)
	fmt.Fprintf(c.stdout, "labels: %s  assignees: %s\n", orDash(strings.Join(issue.Labels, ", ")), orDash(strings.Join(issue.Assignees, ", ")))
	fmt.Fprintf(c.stdout, "parent: %s  children: %s\n", parent, orDash(qualified(children, names(issue.ForeignChildren, false))))
	fmt.Fprintf(c.stdout, "blocked by: %s  blocking: %s\n", orDash(qualified(issue.BlockedBy, names(issue.ForeignBlockers, false))), orDash(refs(blocking)))
	if c.snapshot == nil {
		text := ""
		if body != nil {
			text = *body
		}
		fmt.Fprintf(c.stdout, "\n%s\n", trimRight(text))
		for _, comment := range comments {
			author := "None"
			if comment.Author != nil {
				author = comment.Author.Login
			}
			fmt.Fprintf(c.stdout, "\n--- %s %s %s\n%s\n", author, comment.CreatedAt, comment.URL, trimRight(comment.Body))
		}
	}
	return 0, nil
}

func (c cli) runCheck(document Document, strays []string) int {
	errs, warnings := document.check()
	for _, warning := range append(warnings, strays...) {
		fmt.Fprintf(c.stderr, "warning: %s\n", warning)
	}
	for _, problem := range errs {
		fmt.Fprintf(c.stderr, "error: %s\n", problem)
	}
	source := "github " + document.Repository
	if c.snapshot != nil {
		source = "snapshot " + display(c.root, *c.snapshot)
	}
	if len(errs) > 0 {
		fmt.Fprintf(c.stderr, "wayfinder: %d error(s) in %s\n", len(errs), source)
		return 1
	}
	fmt.Fprintf(c.stdout, "wayfinder: %d issues valid (%s)\n", len(document.Issues), source)
	return 0
}

func (c cli) writeSnapshot(document Document) (int, error) {
	if errs, _ := document.check(); len(errs) > 0 {
		for _, problem := range errs {
			fmt.Fprintf(c.stderr, "error: %s\n", problem)
		}
		fmt.Fprintln(c.stderr, "wayfinder: refusing to snapshot a structurally invalid map; fix GitHub first")
		return 1, nil
	}
	text := render(document)
	shown := display(c.root, c.output)
	if c.checkOutput {
		current, err := os.ReadFile(c.output)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return 1, failf("cannot read %s: %v", shown, err)
		}
		if string(current) != text {
			fmt.Fprintf(c.stderr, "wayfinder: %s is stale; run `go run ./cmd/wayfinder snapshot`\n", shown)
			return 1, nil
		}
		fmt.Fprintf(c.stdout, "wayfinder: %s matches github %s\n", shown, document.Repository)
		return 0, nil
	}
	if err := os.WriteFile(c.output, []byte(text), 0o666); err != nil {
		return 1, failf("cannot write %s: %v", shown, err)
	}
	fmt.Fprintf(c.stdout, "wayfinder: wrote %d issues to %s\n", len(document.Issues), shown)
	return 0, nil
}

func authorValue(comment liveComment) any {
	if comment.Author == nil {
		return nil
	}
	return comment.Author.Login
}

func containsInt(values []int, value int) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func orDash(text string) string {
	if text == "" {
		return "-"
	}
	return text
}

// trimRight matches Python's str.rstrip(): Unicode whitespace plus the ASCII separators U+001C..U+001F.
func trimRight(text string) string {
	return strings.TrimRightFunc(text, func(r rune) bool { return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f) })
}
