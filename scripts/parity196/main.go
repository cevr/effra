// Command parity196 compares the sources the check package's tests check
// at a base commit, in the old syntax, with those they check at a head
// commit, in the decided syntax (#196, parse-v1 round 6). Each input is a
// JSON-lines log that run.sh records, one line per distinct source: the
// test that checked it, the source and its diagnostics.
//
// A base source pairs with the head source that equals its translation
// (migrate196.Translate), byte for byte, or apart from blanks and the
// `extern go fixture.Name` bindings the head generators give bodiless
// functions. A pair keeps
// its verdict when both have the same codes and each old diagnostic has a
// new one of its code on the line its own line maps to; lines map by the
// words (names, numbers, literals) the two texts share, in order. The
// report counts the pairs, lists every changed verdict with both
// diagnostic lists, and lists the sources on either side that pair with
// nothing, by test: a case deleted or rewritten by hand, a generator that
// now writes another program, or a source only one side checks.
//
// usage: parity196 BASE.jsonl HEAD.jsonl
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"ergo.local/prototype/internal/migrate196"
)

type diag struct {
	Code    string `json:"code"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
}

type rec struct {
	Test  string `json:"test"`
	Src   string `json:"src"`
	Diags []diag `json:"diags"`
}

func load(path string) []rec {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	defer f.Close()
	var out []rec
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<30)
	for sc.Scan() {
		var r rec
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			fmt.Fprintln(os.Stderr, path, err)
			os.Exit(2)
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintln(os.Stderr, path, err)
		os.Exit(2)
	}
	return out
}

// fixtureExtern is the extern binding a generator writes for a bodiless
// function (the old //ef:go fixture.Name directive).
var fixtureExtern = regexp.MustCompile(`extern go fixture\.[A-Za-z0-9_]+\s+`)

// looseKey is a source without blanks and without the fixture extern
// bindings and semicolons: the translation of an old bodiless function
// keeps no body and no binding, the head generators bind it, and a
// translation with a note is not formatted, so it keeps its semicolons.
func looseKey(s string) string {
	return strings.ReplaceAll(stripBlanks(fixtureExtern.ReplaceAllString(s, "")), ";", "")
}

func stripBlanks(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

// word is a name, a number or a literal of a source, with its line.
type word struct {
	text string
	line int
}

func words(src string) []word {
	var out []word
	line := 1
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9':
			j := i
			for j < len(src) && (src[j] == '_' || src[j] >= 'a' && src[j] <= 'z' || src[j] >= 'A' && src[j] <= 'Z' || src[j] >= '0' && src[j] <= '9') {
				j++
			}
			out = append(out, word{src[i:j], line})
			i = j
		case c == '"' || c == '`':
			j := i + 1
			for j < len(src) && src[j] != c && src[j] != '\n' {
				if src[j] == '\\' && c == '"' {
					j++
				}
				j++
			}
			// A literal is compared without its quotes: an old quoted
			// import path is a decided unquoted one.
			out = append(out, word{strings.Trim(src[i:min(j+1, len(src))], "\"`"), line})
			for _, ch := range src[i:min(j+1, len(src))] {
				if ch == '\n' {
					line++
				}
			}
			i = j + 1
		default:
			i++
		}
	}
	return out
}

// lineMap maps each old line to the range of new lines its words align
// with: a greedy alignment of the two word lists that skips, on either
// side, the fewest words up to a window.
func lineMap(old, new string) map[int][2]int {
	a, b := words(old), words(new)
	out := map[int][2]int{}
	add := func(l, n int) {
		r, ok := out[l]
		if !ok {
			out[l] = [2]int{n, n}
			return
		}
		out[l] = [2]int{min(r[0], n), max(r[1], n)}
	}
	if len(a)*len(b) <= 4_000_000 {
		// A longest common subsequence, exact for sources of up to
		// about two thousand words each.
		n, m := len(a), len(b)
		dp := make([]int32, (n+1)*(m+1))
		at := func(i, j int) *int32 { return &dp[i*(m+1)+j] }
		for i := n - 1; i >= 0; i-- {
			for j := m - 1; j >= 0; j-- {
				if a[i].text == b[j].text {
					*at(i, j) = *at(i+1, j+1) + 1
				} else {
					*at(i, j) = max(*at(i+1, j), *at(i, j+1))
				}
			}
		}
		for i, j := 0, 0; i < n && j < m; {
			switch {
			case a[i].text == b[j].text:
				add(a[i].line, b[j].line)
				i++
				j++
			case *at(i+1, j) >= *at(i, j+1):
				i++
			default:
				j++
			}
		}
		return out
	}
	const window = 64
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i].text == b[j].text {
			add(a[i].line, b[j].line)
			i++
			j++
			continue
		}
		skipA, skipB := -1, -1
		for k := 1; k < window && j+k < len(b); k++ {
			if b[j+k].text == a[i].text {
				skipB = k
				break
			}
		}
		for k := 1; k < window && i+k < len(a); k++ {
			if a[i+k].text == b[j].text {
				skipA = k
				break
			}
		}
		switch {
		case skipB >= 0 && (skipA < 0 || skipB <= skipA):
			j += skipB
		case skipA >= 0:
			i += skipA
		default:
			i++
			j++
		}
	}
	return out
}

// mapped returns the new-line range of an old line, from its own words or
// from its nearest neighbours that have words.
func mapped(m map[int][2]int, line int) (int, int) {
	if r, ok := m[line]; ok {
		return r[0], r[1]
	}
	lo, hi := 1, 1<<30
	for l := line - 1; l >= 1; l-- {
		if r, ok := m[l]; ok {
			lo = r[1]
			break
		}
	}
	for l := line + 1; l <= line+200; l++ {
		if r, ok := m[l]; ok {
			hi = r[0]
			break
		}
	}
	return lo, hi
}

func codes(ds []diag) string {
	var cs []string
	for _, d := range ds {
		cs = append(cs, d.Code)
	}
	sort.Strings(cs)
	return strings.Join(cs, " ")
}

// sameVerdict reports whether the head diagnostics keep the base ones:
// the same codes, each on its mapped line (one line of slack each way).
func sameVerdict(b, h rec) (bool, string) {
	if codes(b.Diags) != codes(h.Diags) {
		return false, "codes"
	}
	if len(b.Diags) == 0 {
		return true, ""
	}
	// The same messages, apart from the package name and the positions
	// they quote, are the same diagnostics, wherever the lines went.
	if messages(b.Diags) == messages(h.Diags) {
		return true, ""
	}
	m := lineMap(b.Src, h.Src)
	used := make([]bool, len(h.Diags))
	for _, d := range b.Diags {
		lo, hi := mapped(m, d.Line)
		found := false
		for k, e := range h.Diags {
			if !used[k] && e.Code == d.Code && e.Line >= lo-1 && e.Line <= hi+1 {
				used[k], found = true, true
				break
			}
		}
		if !found {
			return false, fmt.Sprintf("line: %s at %d maps to %d-%d", d.Code, d.Line, lo, hi)
		}
	}
	return true, ""
}

var (
	qualifier = regexp.MustCompile(`\b[a-z][a-z0-9_]*\.([A-Z])`)
	position  = regexp.MustCompile(`\b\d+:\d+\b`)
)

// messages lists the codes and messages, each message without the
// package qualifiers and the line:column positions it quotes, sorted.
func messages(ds []diag) string {
	var ms []string
	for _, d := range ds {
		m := qualifier.ReplaceAllString(d.Message, "$1")
		ms = append(ms, d.Code+" "+position.ReplaceAllString(m, "L:C"))
	}
	sort.Strings(ms)
	return strings.Join(ms, "\n")
}

func show(ds []diag) string {
	if len(ds) == 0 {
		return "\t(none)\n"
	}
	var b strings.Builder
	for _, d := range ds {
		fmt.Fprintf(&b, "\t%d:%d %s %s\n", d.Line, d.Column, d.Code, d.Message)
	}
	return b.String()
}

func firstLines(src string, n int) string {
	lines := strings.SplitN(src, "\n", n+1)
	if len(lines) > n {
		lines = append(lines[:n], "…")
	}
	return "\t| " + strings.Join(lines, "\n\t| ") + "\n"
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: parity196 BASE.jsonl HEAD.jsonl")
		os.Exit(2)
	}
	base, head := load(os.Args[1]), load(os.Args[2])
	exact := map[string][]int{}
	loose := map[string][]int{}
	for j, h := range head {
		exact[h.Src] = append(exact[h.Src], j)
		loose[looseKey(h.Src)] = append(loose[looseKey(h.Src)], j)
	}
	headUsed := make([]bool, len(head))
	type pair struct {
		b, h int
		how  string
	}
	var pairs []pair
	unpaired := map[string][]int{}
	refused := map[string]int{}
	for i, b := range base {
		t, _, err := migrate196.Translate(b.Src)
		if err != nil {
			refused[b.Test]++
			unpaired[b.Test] = append(unpaired[b.Test], i)
			continue
		}
		if js, ok := exact[t]; ok {
			pairs = append(pairs, pair{i, js[0], "exact"})
			for _, j := range js {
				headUsed[j] = true
			}
			continue
		}
		if js, ok := loose[looseKey(t)]; ok {
			pairs = append(pairs, pair{i, js[0], "loose"})
			for _, j := range js {
				headUsed[j] = true
			}
			continue
		}
		unpaired[b.Test] = append(unpaired[b.Test], i)
	}
	var changed []string
	exactN, same := 0, 0
	for _, p := range pairs {
		if p.how == "exact" {
			exactN++
		}
		b, h := base[p.b], head[p.h]
		if ok, why := sameVerdict(b, h); ok {
			same++
		} else {
			changed = append(changed, fmt.Sprintf("-- %s (head %s), %s\nold:\n%snew:\n%shead source:\n%s", b.Test, h.Test, why, show(b.Diags), show(h.Diags), firstLines(h.Src, 40)))
		}
	}
	headOnly := map[string][]int{}
	for j, h := range head {
		if !headUsed[j] {
			headOnly[h.Test] = append(headOnly[h.Test], j)
		}
	}
	nUnpaired, nHeadOnly := 0, 0
	for _, v := range unpaired {
		nUnpaired += len(v)
	}
	for _, v := range headOnly {
		nHeadOnly += len(v)
	}
	nRefused := 0
	for _, n := range refused {
		nRefused += n
	}
	fmt.Printf("parity196: base %d sources, head %d sources\n", len(base), len(head))
	fmt.Printf("paired %d (%d byte for byte, %d apart from blanks, semicolons and fixture extern bindings): same verdict %d, changed %d\n", len(pairs), exactN, len(pairs)-exactN, same, len(changed))
	fmt.Printf("base sources paired with nothing: %d (%d of them the translator refuses)\n", nUnpaired, nRefused)
	fmt.Printf("head sources paired with nothing: %d\n", nHeadOnly)
	fmt.Println("\n== changed verdicts")
	sort.Strings(changed)
	for _, c := range changed {
		fmt.Print(c)
	}
	byTest := func(title string, m map[string][]int, recs []rec, sample int) {
		fmt.Printf("\n== %s, by test\n", title)
		var tests []string
		for k := range m {
			tests = append(tests, k)
		}
		sort.Strings(tests)
		for _, k := range tests {
			fmt.Printf("-- %s: %d\n", k, len(m[k]))
			for n, i := range m[k] {
				if n == sample {
					break
				}
				fmt.Printf("  codes [%s]\n%s", codes(recs[i].Diags), firstLines(recs[i].Src, 12))
			}
		}
	}
	byTest("base sources paired with nothing", unpaired, base, 3)
	byTest("head sources paired with nothing", headOnly, head, 3)
}
