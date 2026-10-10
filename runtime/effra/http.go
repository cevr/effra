package effra

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

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
// empty bodies and no Content-Type. Shutdown is ordered: cancellation stops
// admission and interrupts every request scope; once the last of them has
// closed, a response still being written must complete within IdleTimeout,
// after which its connection is aborted, so a client that stops reading
// cannot hold shutdown open and no abort overlaps application cleanup.
func ServeHTTPRequests(address string, limits HTTPLimits, handler func(HTTPRequest) Effect[HTTPResponse], onListen func(string)) Effect[Unit] {
	if err := limits.validate(); err != nil {
		return func(*FiberContext) Exit[Unit] { return Die[Unit](err) }
	}
	server := &http.Server{ReadHeaderTimeout: limits.ReadHeaderTimeout, IdleTimeout: limits.IdleTimeout}
	return serveManaged(address, server, func(server context.Context) http.Handler {
		return &httpTransport{server: server, scopes: newRequestScopes(server), limits: limits, handler: handler, active: make(chan struct{}, limits.MaxActive)}
	}, onListen)
}

type httpTransport struct {
	server  context.Context
	scopes  *requestScopes
	limits  HTTPLimits
	handler func(HTTPRequest) Effect[HTTPResponse]
	active  chan struct{}
}

// requestScopes separates the two shutdown phases. Server cancellation closes
// scope admission and interrupts the open request scopes; drained is
// cancelled once the last of them has closed. Response writers are tracked
// apart from scopes: they only arm their write bound on drained, so waiting
// for a scope never waits for a writer.
type requestScopes struct {
	mu      sync.Mutex
	open    int
	closing bool
	drained context.Context
	drain   context.CancelFunc
}

func newRequestScopes(server context.Context) *requestScopes {
	s := &requestScopes{}
	s.drained, s.drain = context.WithCancel(context.Background())
	context.AfterFunc(server, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.closing = true
		if s.open == 0 {
			s.drain()
		}
	})
	return s
}

// enter opens a request scope unless shutdown has begun.
func (s *requestScopes) enter() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return false
	}
	s.open++
	return true
}

func (s *requestScopes) leave() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.open--
	if s.closing && s.open == 0 {
		s.drain()
	}
}

func (t *httpTransport) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	controller := http.NewResponseController(w)
	status := func(code int, closeConnection bool) func() {
		return func() { writeStatus(w, code, closeConnection) }
	}
	if t.server.Err() != nil {
		t.publish(controller, status(http.StatusServiceUnavailable, true))
		return
	}
	select {
	case t.active <- struct{}{}:
		defer func() { <-t.active }()
	default:
		t.publish(controller, status(http.StatusServiceUnavailable, true))
		return
	}
	if r.ContentLength > t.limits.MaxBodyBytes {
		t.publish(controller, status(http.StatusRequestEntityTooLarge, true))
		return
	}
	_ = controller.SetReadDeadline(time.Now().Add(t.limits.ReadBodyTimeout))
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, t.limits.MaxBodyBytes))
	if err != nil {
		// The deadline stays armed: closing an aborted request drains its
		// unread body, which must not wait on a stalled client.
		var tooLarge *http.MaxBytesError
		var network net.Error
		switch {
		case errors.As(err, &tooLarge):
			t.publish(controller, status(http.StatusRequestEntityTooLarge, true))
		case errors.As(err, &network) && network.Timeout(), r.Context().Err() != nil:
			panic(http.ErrAbortHandler)
		default:
			t.publish(controller, status(http.StatusBadRequest, true))
		}
		return
	}
	// The handler runs without a read deadline so the connection's background
	// read observes only client disconnect, never an elapsed body timer.
	_ = controller.SetReadDeadline(time.Time{})
	request := HTTPRequest{Method: r.Method, Path: requestPath(r.RequestURI), ContentType: r.Header.Get("Content-Type"), Body: body}
	if !t.scopes.enter() {
		t.publish(controller, status(http.StatusServiceUnavailable, true))
		return
	}
	exit := func() Exit[HTTPResponse] {
		defer t.scopes.leave()
		return runRequest(t.server, r, t.handler(request))
	}()
	switch {
	case r.Context().Err() != nil:
		// The client is gone: no response remains possible.
		panic(http.ErrAbortHandler)
	case exit.Interrupted && t.server.Err() != nil:
		t.publish(controller, status(http.StatusServiceUnavailable, true))
	case exit.IsFailure():
		t.publish(controller, status(http.StatusInternalServerError, false))
	default:
		t.publish(controller, func() { writeResponse(w, exit.Value) })
	}
}

// publish writes one response while the transport-drain phase bounds its
// writes: once every request scope has closed after server cancellation, they
// must complete within grace, after which they fail and the connection
// closes, so a client that stops reading cannot hold shutdown open. The bound
// starts only after all owned cleanup, never during another request's. The
// response is flushed before the handler returns, so no byte is written
// outside the bound.
func (s *requestScopes) publish(controller *http.ResponseController, grace time.Duration, write func()) {
	stop := context.AfterFunc(s.drained, func() {
		_ = controller.SetWriteDeadline(time.Now().Add(grace))
	})
	defer stop()
	write()
	_ = controller.Flush()
}

func (t *httpTransport) publish(controller *http.ResponseController, write func()) {
	t.scopes.publish(controller, t.limits.IdleTimeout, write)
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
// their path after the authority. The query is removed first, so a slash
// inside it never becomes the path.
func requestPath(target string) string {
	target, _, _ = strings.Cut(target, "?")
	if scheme := strings.Index(target, "://"); scheme > 0 && !strings.HasPrefix(target, "/") {
		rest := target[scheme+3:]
		slash := strings.IndexByte(rest, '/')
		if slash < 0 {
			return "/"
		}
		return rest[slash:]
	}
	return target
}

func writeStatus(w http.ResponseWriter, status int, closeConnection bool) {
	if closeConnection {
		w.Header().Set("Connection", "close")
	}
	w.Header()["Content-Type"] = nil
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(status)
}

// admissibleHeaderValue is the header-value policy shared by every target:
// visible ASCII, space and horizontal tab. It is the intersection of what Go
// and Node publish unchanged; anything else is an invalid response.
func admissibleHeaderValue(value string) bool {
	for i := 0; i < len(value); i++ {
		if c := value[i]; c != '\t' && (c < ' ' || c > '~') {
			return false
		}
	}
	return true
}

// writeResponse validates the complete response before writing any of it;
// an invalid response becomes an empty 500.
func writeResponse(w http.ResponseWriter, response HTTPResponse) {
	if response.Status < 200 || response.Status > 599 || ((response.Status == http.StatusNoContent || response.Status == http.StatusNotModified) && len(response.Body) > 0) || !admissibleHeaderValue(response.ContentType) {
		writeStatus(w, http.StatusInternalServerError, false)
		return
	}
	if response.ContentType == "" {
		w.Header()["Content-Type"] = nil
	} else {
		w.Header().Set("Content-Type", response.ContentType)
	}
	if response.Status != http.StatusNoContent && response.Status != http.StatusNotModified {
		w.Header().Set("Content-Length", strconv.Itoa(len(response.Body)))
	}
	w.WriteHeader(response.Status)
	_, _ = w.Write(response.Body)
}

// serveManaged owns one listener and its server. Cancellation of the serving
// fiber stops admission, cancels active request scopes through the server
// context, and completes only after every request handler has returned.
func serveManaged(address string, server *http.Server, handler func(context.Context) http.Handler, onListen func(string)) Effect[Unit] {
	return func(fc *FiberContext) Exit[Unit] {
		hostPort, err := parseHTTPAddress(address)
		if err != nil {
			return Fail[Unit]("IoError", err)
		}
		server.Handler = handler(fc.Context())
		listener := Invoke(fc, AcquireRelease("http:"+address, func(context.Context) (net.Listener, error) {
			listener, err := net.Listen("tcp", hostPort)
			if err != nil {
				return nil, httpListenError(address, err)
			}
			return listener, nil
		}, func(listener net.Listener, ctx context.Context) error {
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

// HTTP listen addresses and listener failures are Effra-owned text, the same
// bytes on Go and JavaScript (internal/compiler/prelude/http.mjs). The
// address grammar is `host:port`: the port is 0..65535 in decimal, the host
// is empty (every interface), a name or IPv4 address without a colon, or a
// bracketed IPv6 address. docs/runtime.md lists the listener failure classes.
func parseHTTPAddress(address string) (string, error) {
	invalid := func(reason string) (string, error) {
		return "", errors.New("invalid HTTP address " + QuoteText(address) + ": " + reason)
	}
	var host, port string
	if strings.HasPrefix(address, "[") {
		end := strings.IndexByte(address, ']')
		if end < 0 {
			return invalid("unbalanced brackets")
		}
		host, port = address[1:end], address[end+1:]
		if !strings.HasPrefix(port, ":") {
			return invalid("missing port")
		}
		port = port[1:]
		if host == "" || strings.ContainsAny(host, "[]") || !strings.Contains(host, ":") {
			return invalid("brackets must enclose an IPv6 address")
		}
	} else {
		colon := strings.LastIndexByte(address, ':')
		if colon < 0 {
			return invalid("missing port")
		}
		host, port = address[:colon], address[colon+1:]
		if strings.ContainsAny(host, "[]") {
			return invalid("unbalanced brackets")
		}
		if strings.Contains(host, ":") {
			return invalid("an IPv6 host must be in brackets")
		}
	}
	if !httpPortText(port) {
		return invalid("port must be a decimal number from 0 to 65535")
	}
	return net.JoinHostPort(host, port), nil
}

func httpPortText(port string) bool {
	if port == "" || len(port) > 5 {
		return false
	}
	for _, digit := range port {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	value, err := strconv.Atoi(port)
	return err == nil && value <= 65535
}

// httpListenError classifies a host listener failure, as Erlang's :inet
// reports a POSIX atom rather than host text. An unclassified failure keeps
// the host's text, the one documented exception to byte-identical reports.
func httpListenError(address string, err error) error {
	prefix := "HTTP listen on " + QuoteText(address) + ": "
	var lookup *net.DNSError
	switch {
	case errors.As(err, &lookup):
		return errors.New(prefix + "host lookup failed")
	case errors.Is(err, syscall.EADDRINUSE):
		return errors.New(prefix + "address in use")
	case errors.Is(err, syscall.EADDRNOTAVAIL):
		return errors.New(prefix + "address not available")
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return errors.New(prefix + "permission denied")
	}
	return errors.New(prefix + err.Error())
}
