// Command sketchsize measures the #101 example sketches for the route-1
// against route-2 comparison: for each file it prints the lines that hold
// code (not blank, not only a comment) and the tokens Go's scanner reads,
// comments and inserted semicolons excluded. The sketch syntax is Go-shaped,
// so Go's scanner tokenizes it.
//
//	go run ./prototypes/actorlib/cmd/sketchsize docs/research/actor-library/examples/*.go.sketch
package main

import (
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: sketchsize FILE...")
		os.Exit(2)
	}
	fmt.Println("| Sketch | Code lines | Tokens |")
	fmt.Println("| --- | --- | --- |")
	for _, path := range os.Args[1:] {
		src, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		lines, tokens, err := measure(src)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			os.Exit(1)
		}
		fmt.Printf("| %s | %d | %d |\n", filepath.Base(path), lines, tokens)
	}
}

func measure(src []byte) (lines, tokens int, err error) {
	fset := token.NewFileSet()
	file := fset.AddFile("sketch", fset.Base(), len(src))
	var s scanner.Scanner
	var scanErr error
	s.Init(file, src, func(pos token.Position, msg string) {
		if scanErr == nil {
			scanErr = fmt.Errorf("%s: %s", pos, msg)
		}
	}, 0)
	seen := map[int]bool{}
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		if tok == token.SEMICOLON && lit == "\n" {
			continue // inserted, not written
		}
		tokens++
		seen[file.Line(pos)] = true
	}
	return len(seen), tokens, scanErr
}
