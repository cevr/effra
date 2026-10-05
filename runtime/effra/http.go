package effra

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

// ServeHTTP owns the listener and waits for every request's managed cleanup.
// onListen exposes the bound address (including an OS-selected port).
func ServeHTTP(address string, handler func(string) Effect[string], onListen func(string)) Effect[Unit] {
	return func(fc *FiberContext) Exit[Unit] {
		server := &http.Server{ReadHeaderTimeout: 5 * time.Second}
		server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithCancel(r.Context())
			stop := context.AfterFunc(fc.Context(), cancel)
			defer stop()
			defer cancel()
			exit := RunContext(ctx, func(request *FiberContext) Exit[string] { return Invoke(request, handler(r.URL.Path)) })
			if exit.IsFailure() {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte(exit.Value))
		})
		listener := Invoke(fc, AcquireRelease("http:"+address, func(context.Context) (net.Listener, error) { return net.Listen("tcp", address) }, func(listener net.Listener, ctx context.Context) error {
			// Shutdown closes admission and waits for request handlers, including cleanup.
			err := server.Shutdown(ctx)
			closeErr := listener.Close()
			if err != nil {
				return err
			}
			if errors.Is(closeErr, net.ErrClosed) {
				return nil
			}
			return closeErr
		}))
		if listener.IsFailure() {
			return Propagate[Unit](listener)
		}
		if err := fc.Scope().OnCancel(func() error {
			err := listener.Value.Close()
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}); err != nil {
			return Interrupt[Unit](fc.Context().Err())
		}
		if onListen != nil {
			onListen(listener.Value.Addr().String())
		}
		child := Invoke(fc, Fork(func(child *FiberContext) Exit[Unit] {
			err := server.Serve(listener.Value)
			if child.Context().Err() != nil {
				return Interrupt[Unit](child.Context().Err())
			}
			if errors.Is(err, http.ErrServerClosed) {
				return Succeed(Unit{})
			}
			return Fail[Unit]("IoError", err)
		}))
		if child.IsFailure() {
			return Propagate[Unit](child)
		}
		return Invoke(fc, child.Value.Join())
	}
}
