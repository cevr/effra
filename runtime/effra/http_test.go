package effra

import (
	"context"
	"io"
	"net"
	"net/http"
	"testing"
)

func TestHTTPServesRequestsAndWaitsForRequestCleanupOnShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bound := make(chan string, 1)
	started := make(chan struct{})
	cleanup := make(chan struct{})
	finish := make(chan struct{})
	done := make(chan Exit[Unit], 1)
	go func() {
		done <- RunContext(ctx, ServeHTTP("127.0.0.1:0", func(path string) Effect[string] {
			return func(fc *FiberContext) Exit[string] {
				if path == "/health" {
					return Succeed("ok")
				}
				resource := Invoke(fc, AcquireRelease("request", func(context.Context) (Unit, error) { return Unit{}, nil }, func(Unit, context.Context) error { close(cleanup); <-finish; return nil }))
				if resource.IsFailure() {
					return Propagate[string](resource)
				}
				close(started)
				<-fc.Context().Done()
				return Interrupt[string](fc.Context().Err())
			}
		}, func(address string) { bound <- address }))
	}()
	address := wait(t, bound)
	client := &http.Client{}
	defer client.CloseIdleConnections()
	response, err := client.Get("http://" + address + "/health")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || string(data) != "ok" || response.StatusCode != 200 {
		t.Fatalf("health: %s %v", data, err)
	}
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		response, err := client.Get("http://" + address + "/wait")
		if err == nil {
			response.Body.Close()
		}
	}()
	wait(t, started)
	cancel()
	wait(t, cleanup)
	select {
	case <-done:
		t.Fatal("server returned before request cleanup")
	default:
	}
	close(finish)
	if !wait(t, done).Interrupted {
		t.Fatal("shutdown did not preserve cancellation")
	}
	wait(t, requestDone)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("listener was not released: %v", err)
	}
	listener.Close()
}

func TestHTTPStartupFailureIsTyped(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	out := Run(ServeHTTP(listener.Addr().String(), func(string) Effect[string] { return func(*FiberContext) Exit[string] { return Succeed("unreachable") } }, nil))
	if out.Failure == nil || out.Failure.Tag != "IoError" {
		t.Fatalf("startup failure: %+v", out)
	}
}
