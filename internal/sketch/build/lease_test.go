package build

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// leaseCompiler is the process-scoped identity of the lease helper.
var leaseCompiler = "process:" + strings.Repeat("7", 64)

// leaseHelper is helper mode "lease", the owner of one private root: for
// each line "build" on standard input it builds baseInput under
// leaseCompiler and prints the module directory; for "link" it links the
// last module to a fresh program and prints "linked PATH". It exits at the
// end of its input, or when it is killed.
func leaseHelper() int {
	c := Cache{Root: os.Getenv(rootVariable)}
	in := baseInput()
	in.Compiler = leaseCompiler
	var last Module
	lines := bufio.NewScanner(os.Stdin)
	for lines.Scan() {
		switch lines.Text() {
		case "build":
			module, err := c.Module(in, emitFor(in, nil))
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
			last = module
			fmt.Println(module.Dir)
		case "link":
			output := filepath.Join(os.Getenv(rootVariable), "program")
			if out, err := hostToolchain().Command(last, output).CombinedOutput(); err != nil {
				fmt.Fprintf(os.Stderr, "%v: %s\n", err, out)
				return 1
			}
			fmt.Println("linked " + output)
		}
	}
	return 0
}

// privateRootOf is the private root of compiler under root.
func privateRootOf(root, compiler string) string {
	sum := sha256.Sum256([]byte(compiler))
	return filepath.Join(root, "private", hex.EncodeToString(sum[:16]))
}

// age sets root's modification time past privateLifetime.
func age(t *testing.T, root string) {
	t.Helper()
	old := time.Now().Add(-2 * privateLifetime)
	if err := os.Chtimes(root, old, old); err != nil {
		t.Fatal(err)
	}
}

// sweepUntil sweeps c in a loop, re-ageing root before each sweep, until
// stop is closed, and returns how many sweeps ran.
func sweepUntil(c Cache, root string, stop <-chan struct{}) *atomic.Int64 {
	var n atomic.Int64
	go func() {
		old := time.Now().Add(-2 * privateLifetime)
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = os.Chtimes(root, old, old)
			c.sweep(time.Now())
			n.Add(1)
		}
	}()
	return &n
}

// TestPrivateRootLease: a private root is removed only when no live process
// owns it (the stage's round 1 review, M1). An owner process that has been
// idle past privateLifetime keeps its root; it renews the root while sweeps
// run; its root stays while it links the module after the build; and once
// the owner is killed, the next sweep removes the root, aged as it is.
func TestPrivateRootLease(t *testing.T) {
	if !leasesSupported {
		t.Skip("no flock here: private roots are never collected")
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	c := Cache{Root: root}
	owner := exec.Command(binary, "-test.run=^$")
	owner.Env = append(os.Environ(), helperVariable+"=lease", rootVariable+"="+root)
	stdin, err := owner.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := owner.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	owner.Stderr = os.Stderr
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	killed := false
	defer func() {
		if !killed {
			_ = owner.Process.Kill()
			_ = owner.Wait()
		}
	}()
	replies := bufio.NewScanner(stdout)
	ask := func(line string) string {
		t.Helper()
		if _, err := io.WriteString(stdin, line+"\n"); err != nil {
			t.Fatal(err)
		}
		if !replies.Scan() {
			t.Fatalf("the owner did not answer %q: %v", line, replies.Err())
		}
		return replies.Text()
	}
	private := privateRootOf(root, leaseCompiler)
	exists := func() bool { _, err := os.Stat(filepath.Join(private, leaseName)); return err == nil }

	first := ask("build")
	if !strings.HasPrefix(first, private+string(filepath.Separator)) {
		t.Fatalf("the owner built %s, want a module under %s", first, private)
	}

	t.Run("a stale live owner keeps its root", func(t *testing.T) {
		for range 3 {
			age(t, private)
			c.sweep(time.Now())
		}
		if !exists() {
			t.Fatal("a sweep removed the root of a live owner")
		}
	})

	t.Run("an owner renews its root while sweeps run", func(t *testing.T) {
		stop := make(chan struct{})
		sweeps := sweepUntil(c, private, stop)
		for range 10 {
			if dir := ask("build"); dir != first {
				close(stop)
				t.Fatalf("the owner's build gave %s, want its module %s", dir, first)
			}
		}
		close(stop)
		if !exists() || sweeps.Load() == 0 {
			t.Fatalf("root present %v after %d concurrent sweeps", exists(), sweeps.Load())
		}
	})

	t.Run("a root stays while its owner links", func(t *testing.T) {
		stop := make(chan struct{})
		sweeps := sweepUntil(c, private, stop)
		reply := ask("link")
		close(stop)
		program, ok := strings.CutPrefix(reply, "linked ")
		if !ok {
			t.Fatalf("the owner answered %q", reply)
		}
		in := baseInput()
		in.Compiler = leaseCompiler
		if got := runProgram(t, program); got != expectedOutput(t, in, "r1", "unset") || !exists() {
			t.Fatalf("after %d sweeps: program printed %q, root present %v", sweeps.Load(), got, exists())
		}
	})

	t.Run("an abandoned root is removed", func(t *testing.T) {
		if err := owner.Process.Kill(); err != nil {
			t.Fatal(err)
		}
		_ = owner.Wait()
		killed = true
		age(t, private)
		c.sweep(time.Now())
		if _, err := os.Stat(private); !os.IsNotExist(err) {
			t.Fatalf("the abandoned root is still there: %v", err)
		}
		if names := entries(t, root, "private"); len(names) != 0 {
			t.Fatalf("the sweep left %v", names)
		}
	})
}

// TestLeaseRaceWithSweep: an owner taking a root while a sweep collects it
// never ends up holding a lease on a root that is gone. Each round ages an
// ownerless root and runs takeLease and collect at once; whatever the order,
// a lease that takeLease returns names the root's current lease file.
func TestLeaseRaceWithSweep(t *testing.T) {
	if !leasesSupported {
		t.Skip("no flock here")
	}
	root := filepath.Join(t.TempDir(), "private", "r")
	removed := 0
	for range 300 {
		f, err := takeLease(root)
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
		age(t, root)
		var wg sync.WaitGroup
		var owned *os.File
		var takeErr error
		wg.Add(2)
		go func() { defer wg.Done(); owned, takeErr = takeLease(root) }()
		go func() {
			defer wg.Done()
			if collect(root) {
				removed++
			}
		}()
		wg.Wait()
		if takeErr != nil {
			t.Fatal(takeErr)
		}
		if !sameLease(owned, filepath.Join(root, leaseName)) {
			t.Fatal("the owner holds a lease on a root a sweep removed")
		}
		if collect(root) {
			t.Fatal("a sweep removed a root its owner holds")
		}
		owned.Close()
	}
	t.Logf("300 rounds: the sweep won %d", removed)
}
