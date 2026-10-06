package producer

import (
	"errors"
	"io"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestAcquisitionIsOnceAndIdentifiesBytesRatherThanDeclarations(t *testing.T) {
	var opens atomic.Int32
	get := acquireOnce(func() (io.ReadCloser, error) {
		if opens.Add(1) != 1 {
			return nil, errors.New("second acquisition forbidden")
		}
		return io.NopCloser(strings.NewReader("compiler A")), nil
	}, func() (*debug.BuildInfo, bool) { return &debug.BuildInfo{GoVersion: "go-test"}, true }, func() (string, error) { return "test", nil })
	var wg sync.WaitGroup
	identities := make(chan Identity, 32)
	for range 32 {
		wg.Go(func() { identities <- get() })
	}
	wg.Wait()
	close(identities)
	first := get()
	for identity := range identities {
		if identity != first {
			t.Fatal("unstable qualification")
		}
	}
	if opens.Load() != 1 || first.Strength != "executing-artifact" || first.Qualifier != first.Digest || first.ReuseScope != "artifact" {
		t.Fatal(first, opens.Load())
	}
	other := acquireOnce(func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("compiler B")), nil }, func() (*debug.BuildInfo, bool) { return &debug.BuildInfo{GoVersion: "go-test"}, true }, func() (string, error) { return "test", nil })()
	if other.Declaration != first.Declaration || other.Digest == first.Digest {
		t.Fatal("declaration used as content identity")
	}
}

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, errors.New("private host error") }
func (brokenReader) Close() error             { return nil }

func TestUnavailableQualificationAndBoundedDeclarations(t *testing.T) {
	for _, open := range []func() (io.ReadCloser, error){func() (io.ReadCloser, error) { return nil, errors.New("private path") }, func() (io.ReadCloser, error) { return brokenReader{}, nil }, func() (io.ReadCloser, error) { return openImage("unsupported") }} {
		var count atomic.Int32
		get := acquireOnce(func() (io.ReadCloser, error) { count.Add(1); return open() }, func() (*debug.BuildInfo, bool) { return nil, false }, func() (string, error) { return "nonce-A", nil })
		first, second := get(), get()
		if first != second || count.Load() != 1 || first.Strength != "unavailable" || first.Digest != "" || first.ReuseScope != "process" || first.Qualifier != "process:nonce-A" || strings.Contains(first.Reason, "private") {
			t.Fatal(first, second, count.Load())
		}
	}
	none := acquireOnce(func() (io.ReadCloser, error) { return nil, errUnsupported }, func() (*debug.BuildInfo, bool) { return nil, false }, func() (string, error) { return "", errors.New("no entropy") })()
	if none.ReuseScope != "none" || none.Qualifier != "" || none.Strength != "unavailable" {
		t.Fatal(none)
	}
	info := &debug.BuildInfo{GoVersion: "go-test", Main: debug.Module{Path: strings.Repeat("x", 257)}, Settings: []debug.BuildSetting{{Key: "-ldflags", Value: "secret"}, {Key: "vcs.modified", Value: "true"}, {Key: "GOOS", Value: "linux"}, {Key: "CGO_ENABLED", Value: "bad"}}}
	d := declaration(info, true)
	if d.Module != "" || d.VCSModified != "true" || d.GOOS != "linux" || d.CGOEnabled != "" || declaration(nil, true) != (Declaration{}) || declaration(info, false) != (Declaration{}) {
		t.Fatal(d)
	}
}

func TestCurrentIdentifiesEmbeddingHost(t *testing.T) {
	id := Current()
	if runtime.GOOS == "linux" && id.Strength != "executing-artifact" {
		t.Fatal(id)
	}
	if id != Current() {
		t.Fatal("unstable current identity")
	}
}
