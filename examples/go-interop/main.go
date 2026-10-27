package main

import (
	"bytes"
	"context"
	"ergo.local/prototype/runtime/ergo"
	"fmt"
	"io"
)

func main() {
	// The real Go API returns a partial count together with an error.
	// No invented success/error union hides either return value.
	buffer := make([]byte, 8)
	read := ergo.FromGo(func(context.Context) (int, error) {
		return io.ReadFull(bytes.NewBufferString("abc"), buffer)
	})
	result := ergo.Run(read)
	if result.IsFailure() {
		panic(result.Cause().Error())
	}
	fmt.Printf("raw: count=%d data=%q error=%v\n", result.Value.Value,
		buffer[:result.Value.Value], result.Value.Err)

	// Error-channel adaptation is explicit; its payload retains the partial value.
	checked := ergo.Run(ergo.OrFail(ergo.FromGo(func(context.Context) (int, error) {
		return io.ReadFull(bytes.NewBufferString("abc"), buffer)
	})))
	payload := checked.Failure.Payload.(ergo.GoError)
	fmt.Printf("adapted: %s partial=%v error=%v\n", checked.Failure.Tag, payload.Partial, payload.Err)

	// A context-aware SDK can observe cancellation without spawning an orphan goroutine.
	wait := ergo.FromGo(func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	exit := ergo.Run(ergo.Timeout(ergo.OrFail(wait), 1))
	fmt.Printf("cancelled SDK: %s\n", exit.Failure.Tag)
}
