package effra

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// ServeHTTP owns the listener and waits for every request's managed cleanup.
// onListen exposes the bound address (including an OS-selected port). It is
// the raw path-to-text transport control: no limits, plain text responses.
func ServeHTTP(address string, handler func(string) Effect[string], onListen func(string)) Effect[Unit] {
	return serveManaged(address, &http.Server{ReadHeaderTimeout: 5 * time.Second}, func(server context.Context) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			exit := runRequest(server, r, handler(r.URL.Path))
			if exit.IsFailure() {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte(exit.Value))
		})
	}, onListen)
}

// HTTPRequest is one admitted request with its complete buffered body. Path
// is the exact request-target path as received, without query or decoding.
// ContentType is empty when the request carries no Content-Type header.
type HTTPRequest struct {
	Method      string
	Path        string
	ContentType string
	Body        []byte
}

// HTTPResponse is published only after the request scope has closed. An
// empty ContentType omits the header; the transport never sniffs one.
type HTTPResponse struct {
	Status      int
	ContentType string
	Body        []byte
}

// HTTPLimits are the explicit bounds of the managed buffered transport. Every
// field is required: there is no default body size or timeout.
type HTTPLimits struct {
	MaxBodyBytes      int64
	ReadHeaderTimeout time.Duration
	ReadBodyTimeout   time.Duration
	IdleTimeout       time.Duration
	MaxActive         int
}

// maxHTTPMillis and maxHTTPCount are the largest timer and count every target
// represents exactly; source limits outside them are rejected, not clamped.
const (
	maxHTTPMillis = 1<<31 - 1
	maxHTTPCount  = 1<<53 - 1
)

// HTTPLimitsFromMillis admits source limits expressed in bytes, milliseconds
// and requests.
func HTTPLimitsFromMillis(maxBodyBytes, readHeaderMillis, readBodyMillis, idleMillis, maxActive int64) (HTTPLimits, error) {
	for _, millis := range []int64{readHeaderMillis, readBodyMillis, idleMillis} {
		if millis < 1 || millis > maxHTTPMillis {
			return HTTPLimits{}, fmt.Errorf("invalid HTTP limits: timeouts must be within 1..%d milliseconds", maxHTTPMillis)
		}
	}
	if maxBodyBytes < 0 || maxBodyBytes > maxHTTPCount || maxActive < 1 || maxActive > maxHTTPMillis {
		return HTTPLimits{}, fmt.Errorf("invalid HTTP limits: body %d bytes, active %d", maxBodyBytes, maxActive)
	}
	return HTTPLimits{MaxBodyBytes: maxBodyBytes, ReadHeaderTimeout: time.Duration(readHeaderMillis) * time.Millisecond, ReadBodyTimeout: time.Duration(readBodyMillis) * time.Millisecond, IdleTimeout: time.Duration(idleMillis) * time.Millisecond, MaxActive: int(maxActive)}, nil
}

func (l HTTPLimits) validate() error {
	if l.MaxBodyBytes < 0 || l.ReadHeaderTimeout <= 0 || l.ReadBodyTimeout <= 0 || l.IdleTimeout <= 0 || l.MaxActive < 1 {
		return fmt.Errorf("invalid HTTP limits: body %d bytes, header %s, body read %s, idle %s, active %d", l.MaxBodyBytes, l.ReadHeaderTimeout, l.ReadBodyTimeout, l.IdleTimeout, l.MaxActive)
	}
	return nil
}

// ServeHTTPRequests is the managed buffered HTTP/1.1 transport. Transport
// policy owns admission, body bounds and fail-closed responses; the handler
// owns routing and every other response, including the profile's 400, 404
// and 415 replies.
//
//   - admission beyond MaxActive, or after shutdown began: 503, no handler
//   - declared or streamed body beyond MaxBodyBytes: 413, no handler
//   - malformed body framing: 400, no handler
//   - body not received within ReadBodyTimeout: connection closed, no response
//   - handler failure, defect or cleanup failure, invalid response: 500
//   - server cancellation: 503 if the client is still connected
//   - client disconnect: handler cancelled, no further bytes
//
// Every response is written only after the request scope, including the
// handler's resources and children, has closed. Status-only responses have
// empty bodies and no Content-Type.
func ServeHTTPRequests(address string, limits HTTPLimits, handler func(HTTPRequest) Effect[HTTPResponse], onListen func(string)) Effect[Unit] {
	if err := limits.validate(); err != nil {
		return func(*FiberContext) Exit[Unit] { return Die[Unit](err) }
	}
	server := &http.Server{ReadHeaderTimeout: limits.ReadHeaderTimeout, IdleTimeout: limits.IdleTimeout}
	return serveManaged(address, server, func(server context.Context) http.Handler {
		return &httpTransport{server: server, limits: limits, handler: handler, active: make(chan struct{}, limits.MaxActive)}
	}, onListen)
}

type httpTransport struct {
	server  context.Context
	limits  HTTPLimits
	handler func(HTTPRequest) Effect[HTTPResponse]
	active  chan struct{}
}

func (t *httpTransport) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if t.server.Err() != nil {
		writeStatus(w, http.StatusServiceUnavailable, true)
		return
	}
	select {
	case t.active <- struct{}{}:
		defer func() { <-t.active }()
	default:
		writeStatus(w, http.StatusServiceUnavailable, true)
		return
	}
	if r.ContentLength > t.limits.MaxBodyBytes {
		writeStatus(w, http.StatusRequestEntityTooLarge, true)
		return
	}
	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(time.Now().Add(t.limits.ReadBodyTimeout))
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, t.limits.MaxBodyBytes))
	if err != nil {
		// The deadline stays armed: closing an aborted request drains its
		// unread body, which must not wait on a stalled client.
		var tooLarge *http.MaxBytesError
		var network net.Error
		switch {
		case errors.As(err, &tooLarge):
			writeStatus(w, http.StatusRequestEntityTooLarge, true)
		case errors.As(err, &network) && network.Timeout(), r.Context().Err() != nil:
			panic(http.ErrAbortHandler)
		default:
			writeStatus(w, http.StatusBadRequest, true)
		}
		return
	}
	// The handler runs without a read deadline so the connection's background
	// read observes only client disconnect, never an elapsed body timer.
	_ = controller.SetReadDeadline(time.Time{})
	request := HTTPRequest{Method: r.Method, Path: requestPath(r.RequestURI), ContentType: r.Header.Get("Content-Type"), Body: body}
	exit := runRequest(t.server, r, t.handler(request))
	switch {
	case r.Context().Err() != nil:
		// The client is gone: no response remains possible.
		panic(http.ErrAbortHandler)
	case exit.Interrupted && t.server.Err() != nil:
		writeStatus(w, http.StatusServiceUnavailable, true)
	case exit.IsFailure():
		writeStatus(w, http.StatusInternalServerError, false)
	default:
		writeResponse(w, exit.Value)
	}
}

// runRequest executes one handler in a fresh request scope linked to both the
// client connection and server cancellation. It returns only after that scope
// has closed, so the caller publishes nothing before owned cleanup completes.
func runRequest[A any](server context.Context, r *http.Request, program Effect[A]) Exit[A] {
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(server, cancel)
	defer stop()
	defer cancel()
	return RunContext(ctx, func(request *FiberContext) Exit[A] { return Invoke(request, program) })
}

// requestPath returns the path component of a request target exactly as
// received. Origin-form targets keep their bytes; absolute-form targets use
// their path after the authority.
func requestPath(target string) string {
	if scheme := strings.Index(target, "://"); scheme > 0 && !strings.HasPrefix(target, "/") {
		rest := target[scheme+3:]
		slash := strings.IndexByte(rest, '/')
		if slash < 0 {
			return "/"
		}
		target = rest[slash:]
	}
	path, _, _ := strings.Cut(target, "?")
	return path
}

func writeStatus(w http.ResponseWriter, status int, closeConnection bool) {
	if closeConnection {
		w.Header().Set("Connection", "close")
	}
	w.Header()["Content-Type"] = nil
	w.WriteHeader(status)
}

func writeResponse(w http.ResponseWriter, response HTTPResponse) {
	if response.Status < 200 || response.Status > 599 || ((response.Status == http.StatusNoContent || response.Status == http.StatusNotModified) && len(response.Body) > 0) || strings.ContainsAny(response.ContentType, "\r\n") {
		writeStatus(w, http.StatusInternalServerError, false)
		return
	}
	if response.ContentType == "" {
		w.Header()["Content-Type"] = nil
	} else {
		w.Header().Set("Content-Type", response.ContentType)
	}
	w.WriteHeader(response.Status)
	_, _ = w.Write(response.Body)
}

// serveManaged owns one listener and its server. Cancellation of the serving
// fiber stops admission, cancels active request scopes through the server
// context, and completes only after every request handler has returned.
func serveManaged(address string, server *http.Server, handler func(context.Context) http.Handler, onListen func(string)) Effect[Unit] {
	return func(fc *FiberContext) Exit[Unit] {
		server.Handler = handler(fc.Context())
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
