// Command checkout is the checkout example in plain Go, for the README comparison.
// The gate vets it with the rest of the module, and scripts/readme_smoke.py runs it and
// compares its output with the Effra and TypeScript versions.
package main

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type OrderNotFoundError struct{ ID string }

func (e *OrderNotFoundError) Error() string { return "order not found: " + e.ID }

var ErrGatewayDown = errors.New("gateway down")

type Order struct {
	ID    string
	Total int64
}

// Payment is a closed set only by convention: any type with isPayment satisfies it.
type Payment interface{ isPayment() }

type Pending struct{}
type Authorized struct{ AuthID string }
type Declined struct{ Reason string }

func (Pending) isPayment()    {}
func (Authorized) isPayment() {}
func (Declined) isPayment()   {}

type Orders interface {
	Find(ctx context.Context, id string) (Order, error)
}

type Gateway interface {
	Authorize(ctx context.Context, order Order) (Payment, error)
}

// Checkout can fail with *OrderNotFoundError, ErrGatewayDown or context.DeadlineExceeded,
// but the signature only says error, and only this comment says which.
func Checkout(ctx context.Context, orders Orders, gateway Gateway, id string) (string, error) {
	order, err := orders.Find(ctx, id)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	payment, err := gateway.Authorize(ctx, order)
	if err != nil {
		return "", err
	}
	switch p := payment.(type) {
	case Pending:
		return "pending", nil
	case Authorized:
		return "paid " + p.AuthID, nil
	case Declined:
		return "declined: " + p.Reason, nil
	default: // the compiler cannot prove this unreachable; nil also lands here
		return "", fmt.Errorf("unknown payment %T", payment)
	}
}

type memoryOrders struct{}

func (memoryOrders) Find(_ context.Context, id string) (Order, error) {
	if id == "42" {
		return Order{ID: id, Total: 1999}, nil
	}
	return Order{}, &OrderNotFoundError{ID: id}
}

type fakeGateway struct{ authID string }

func (g fakeGateway) Authorize(context.Context, Order) (Payment, error) {
	return Authorized{AuthID: g.authID}, nil
}

func report(ctx context.Context, id string) {
	outcome, err := Checkout(ctx, memoryOrders{}, fakeGateway{authID: "auth-7"}, id)
	var notFound *OrderNotFoundError
	switch {
	case errors.As(err, &notFound):
		outcome = "no such order"
	case errors.Is(err, ErrGatewayDown):
		outcome = "gateway down"
	case errors.Is(err, context.DeadlineExceeded):
		outcome = "gateway timed out"
	case err != nil:
		outcome = "unexpected: " + err.Error()
	}
	fmt.Println(outcome)
}

func main() {
	ctx := context.Background()
	report(ctx, "42")
	report(ctx, "7")
}
