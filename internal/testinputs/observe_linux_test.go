//go:build linux && (amd64 || arm64)

package testinputs

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// run interprets the filter for one call, as the kernel's classic BPF
// interpreter would on struct seccomp_data, and returns the action.
func run(t *testing.T, program []sockFilter, arch, nr uint32, arg0 uint64) uint32 {
	t.Helper()
	var data [64]byte
	binary.LittleEndian.PutUint32(data[0:], nr)
	binary.LittleEndian.PutUint32(data[4:], arch)
	binary.LittleEndian.PutUint64(data[16:], arg0)
	var a uint32
	for pc := 0; pc < len(program); pc++ {
		in := program[pc]
		switch in.code {
		case 0x20: // ld [k]
			a = binary.LittleEndian.Uint32(data[in.k:])
		case 0x15, 0x25, 0x35, 0x45: // jeq, jgt, jge, jset
			taken := a == in.k
			switch in.code {
			case 0x25:
				taken = a > in.k
			case 0x35:
				taken = a >= in.k
			case 0x45:
				taken = a&in.k != 0
			}
			if taken {
				pc += int(in.jt)
			} else {
				pc += int(in.jf)
			}
		case 0x06: // ret k
			return in.k
		default:
			t.Fatalf("instruction %d: unknown code %#x", pc, in.code)
		}
	}
	t.Fatal("the program fell off its end")
	return 0
}

func TestFilter(t *testing.T) {
	program := filter()
	if len(program) > 4096 {
		t.Fatalf("%d instructions exceed BPF_MAXINSNS", len(program))
	}
	notify := uint32(seccompRetUserNotif)
	allow := uint32(seccompRetAllow)
	for _, c := range calls {
		want := notify
		switch c.kind {
		case kindRefused:
			want = seccompRetErrno | uint32(syscall.ENOSYS)
		case kindNamespaceClone:
			if got := run(t, program, auditArch, c.nr, uint64(syscall.SIGCHLD)); got != allow {
				t.Errorf("a plain clone: action %#x, want allow", got)
			}
			want = notify
			if got := run(t, program, auditArch, c.nr, syscall.CLONE_NEWUSER|uint64(syscall.SIGCHLD)); got != want {
				t.Errorf("clone(CLONE_NEWUSER): action %#x, want notify", got)
			}
			continue
		}
		if got := run(t, program, auditArch, c.nr, 0); got != want {
			t.Errorf("%d (kind %d): action %#x, want %#x", c.nr, c.kind, got, want)
		}
	}
	for _, nr := range []uint32{0, 1, 3, 9, 60, 63, 231, highestKnownCall} { // read, write, close, mmap, exit... on amd64; others on arm64
		if _, ok := callsByNumber[int32(nr)]; ok {
			continue
		}
		if got := run(t, program, auditArch, nr, 0); got != allow {
			t.Errorf("unlisted call %d: action %#x, want allow", nr, got)
		}
	}
	// A call newer than the table (getxattrat, open_tree_attr, file_getattr)
	// is notified, and the observer refuses what it cannot classify.
	for _, nr := range []uint32{highestKnownCall + 1, 464, 467, 468, 1023} {
		if got := run(t, program, auditArch, nr, 0); got != notify {
			t.Errorf("call %d, newer than the table: action %#x, want notify", nr, got)
		}
	}
	for _, c := range calls {
		if c.nr > highestKnownCall {
			t.Errorf("call %d (%s) lies above highestKnownCall", c.nr, c.name)
		}
	}
	// A call of another architecture (i386 on amd64, arm on arm64) is
	// notified, and the observer refuses what it cannot decode.
	if got := run(t, program, 0x40000003, 1, 0); got != notify {
		t.Errorf("foreign architecture: action %#x, want notify", got)
	}
	if checkX32SyscallBits {
		if got := run(t, program, auditArch, x32SyscallBit|1, 0); got != notify {
			t.Errorf("x32 call: action %#x, want notify", got)
		}
	}
}

// TestShebangNeverBlocks checks that reading the interpreter line of an
// executed path skips a FIFO instead of waiting for a writer, and still
// follows a script's absolute interpreter.
func TestShebangNeverBlocks(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o755); err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() { done <- shebang(fifo) }()
	select {
	case got := <-done:
		if got != "" {
			t.Fatalf("a FIFO named the interpreter %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reading a FIFO's interpreter line blocked")
	}
	script := filepath.Join(dir, "script")
	if err := os.WriteFile(script, []byte("#!/nonexistent/loop-probe-x -e\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := shebang(script); got != "/nonexistent/loop-probe-x" {
		t.Fatalf("the script's interpreter is %q", got)
	}
}

// TestMagicLinks checks that a path through a process's working directory,
// root or descriptor in /proc, or through /dev/fd, becomes the path of the
// file the kernel reaches.
func TestMagicLinks(t *testing.T) {
	dir := t.TempDir()
	file, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	fd := strconv.Itoa(int(file.Fd()))
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	tid := syscall.Gettid()
	pid := strconv.Itoa(os.Getpid())
	for _, c := range []struct{ path, want string }{
		{"/proc/self/cwd/a/b", cwd + "/a/b"},
		{"/proc/thread-self/cwd/a", cwd + "/a"},
		{"/proc/" + pid + "/task/" + strconv.Itoa(tid) + "/root/etc/hosts", "/etc/hosts"},
		{"/proc/self/fd/" + fd + "/x.txt", dir + "/x.txt"},
		{"/dev/fd/" + fd + "/sub/x.txt", dir + "/sub/x.txt"},
		{"/proc/self/fd/" + fd, "/proc/self/fd/" + fd}, // a reopen: the open by path was recorded
		{"/proc/self/status", "/proc/self/status"},
		{"/proc/self/fd/999999/x", "/proc/self/fd/999999/x"}, // closed: the call fails too
		{"/dev/null", "/dev/null"},
		{"/dev/stdin/x", "/dev/stdin/x"},
		{"/usr/lib/x", "/usr/lib/x"},
	} {
		if got := magic(tid, c.path); got != c.want {
			t.Errorf("magic(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

// TestWorkdirs checks that a name relative to a working directory reached
// through a link is also recorded under the link, whichever process makes
// the call, and under every link that led there.
func TestWorkdirs(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "a")
	link := filepath.Join(base, "current")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a", link); err != nil {
		t.Fatal(err)
	}
	physical, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatal(err)
	}
	o := newObserver(base)
	if got := o.dirs.logical(physical); got != nil {
		t.Fatalf("before any chdir: %q", got)
	}
	o.dirs.change(filepath.Join(base, "missing")) // fails: nothing to remember
	o.dirs.change(target)                         // crosses no link
	o.dirs.change("/proc/self/cwd")               // a magic link, which resolves in the observer
	if o.dirs.active.Load() {
		t.Fatal("a chdir through no link was remembered")
	}
	o.dirs.change(link)
	o.dirs.change(link) // remembered once
	if got := o.dirs.logical(physical); !slices.Equal(got, []string{link}) {
		t.Fatalf("after chdir: %q, want %q", got, link)
	}
	if got := o.dirs.logical(base); got != nil {
		t.Fatalf("at another physical location: %q", got)
	}
	// Any thread, a reparented child's included: nothing is kept per process.
	const child = 1 << 30 // no such thread
	for _, tid := range []int{syscall.Gettid(), child} {
		got := o.names(tid, located{path: physical + "/value.txt", base: physical, name: "value.txt"})
		if want := []string{physical + "/value.txt", link + "/value.txt"}; !slices.Equal(got, want) {
			t.Fatalf("names in %d: %q, want %q", tid, got, want)
		}
	}
	other := filepath.Join(base, "other")
	if err := os.Symlink(target, other); err != nil {
		t.Fatal(err)
	}
	o.dirs.change(other)
	if got, want := o.dirs.logical(physical), []string{link, other}; !slices.Equal(got, want) {
		t.Fatalf("after a second link: %q, want %q", got, want)
	}
}
