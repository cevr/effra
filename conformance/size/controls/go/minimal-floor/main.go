// Command minimal-floor is an unmatched floor for fixtures/minimal.ef: it
// prints the same result without the entry contract (no signal
// cancellation, no owning scope). It bounds the size of the output alone and
// is never compared as an equivalent program.
package main

import "fmt"

func main() {
	fmt.Println("minimal")
}
