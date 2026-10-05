package sdk

import (
	"context"
	"errors"
)

// Lookup is an ordinary context-aware Go SDK function, independent of Effra.
func Lookup(ctx context.Context, id string) (string, error) {
	if id == "slow" {
		<-ctx.Done()
		return "partial", ctx.Err()
	}
	if id == "missing" {
		return "partial", errors.New("not found")
	}
	return "Ada", nil
}
