//go:build linux && (amd64 || arm64)

package testinputs

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// The observation uses seccomp user notification (seccomp_unotify(2)). The
// test process installs a filter that suspends each file system call of
// interest, in itself and in every process it starts, and hands the
// filter's listener to the observing parent. The parent reads the call's
// path from the suspended thread and lets the call continue unchanged. No
// thread is ptrace-stopped, so signals, threads and debuggers behave as
// without observation, and several parent threads serve calls at once.

// A kind says what a notified system call reveals.
type kind uint8

const (
	kindOpen           kind = iota + 1 // reads a file or lists a directory, unless opened write-only
	kindOpenat2                        // openat with its flags in a struct open_how
	kindStat                           // consults a path's existence or metadata
	kindChdir                          // looks a directory up and makes it the working directory
	kindFchdir                         // makes a descriptor's directory the working directory
	kindExec                           // runs a file with an environment
	kindClone3                         // may create namespaces; its flags are in memory
	kindSeccomp                        // may install a listener that takes calls from this one
	kindNamespaceClone                 // clone with namespace flags (the filter passes others)
	kindUnobservable                   // makes later paths mean something the observer cannot see
	kindRefused                        // fails with ENOSYS in the process, without a notification
)

// A call describes a notified system call: which argument holds the
// directory descriptor (-1 for the working directory), the path, the open
// flags (or struct open_how), the AT_* flags (0 for none) and the
// environment. A call that names two paths (rename, link) has second set,
// with the second path's directory descriptor and path in dirfd2 and path2.
type call struct {
	nr                               uint32
	kind                             kind
	dirfd, path, flags, atFlags, env int
	second                           bool
	dirfd2, path2                    int
	name                             string
}

const (
	prSetNoNewPrivs           = 38
	seccompModeFilter         = 1
	seccompFlagTsync          = 1 << 0
	seccompFlagNewListener    = 1 << 3
	seccompFlagTsyncESRCH     = 1 << 4
	seccompFlagWaitKillable   = 1 << 5
	seccompRetAllow           = 0x7fff0000
	seccompRetUserNotif       = 0x7fc00000
	seccompRetErrno           = 0x00050000
	seccompIoctlNotifRecv     = 0xc0502100
	seccompIoctlNotifSend     = 0xc0182101
	seccompIoctlNotifIDValid  = 0x40082102
	seccompNotifContinue      = 1
	seccompIoctlNotifSetFlags = 0x40082104
	seccompNotifSyncWakeUp    = 1
	atFdcwd                   = -100
	atEmptyPath               = 0x1000
	resolveInRoot             = 0x10
	pageSize                  = 4096
	pathMax                   = 4096
	// namespaceFlags are the clone flags that make a namespace: CLONE_NEWNS,
	// NEWCGROUP, NEWUTS, NEWIPC, NEWUSER, NEWPID and NEWNET.
	namespaceFlags = 0x00020000 | 0x02000000 | 0x04000000 | 0x08000000 | 0x10000000 | 0x20000000 | 0x40000000
)

// listenerVariable names the descriptor on which the test process sends its
// filter's listener to the observing parent.
const listenerVariable = "EFFRA_TESTINPUTS_LISTENER"

// filteredVariable marks the test process that the filtered child started
// by exec, with the filter in place from its first instruction.
const filteredVariable = "EFFRA_TESTINPUTS_FILTERED"

// armedPath is the path the test process looks up once its listener is
// handed over. The parent sees the lookup only if the filter reports it.
const armedPath = "/nonexistent/effra-testinputs/armed"

// observed is true in the observed test process, which runs the tests itself.
var observed bool

// init runs in the child the observer starts. It installs the filter, hands
// its listener over, and runs the test binary again by exec: the filter
// survives exec, so the package initializers that Go ran before this one
// run again observed. Only the exec'd process reports itself armed; if the
// exec fails, the observer finds no armed report and refuses the result.
func init() {
	if _, ok := os.LookupEnv(filteredVariable); ok {
		os.Unsetenv(filteredVariable)
		observed = true
		syscall.Access(armedPath, 0)
		return
	}
	fd, ok := os.LookupEnv(listenerVariable)
	if !ok {
		return
	}
	os.Unsetenv(listenerVariable)
	observed = true
	number, err := strconv.Atoi(fd)
	if err != nil {
		return
	}
	socket := os.NewFile(uintptr(number), "testinputs")
	listener, err := installFilter()
	if err != nil {
		socket.Write([]byte(err.Error()))
		socket.Close()
		return
	}
	rights := syscall.UnixRights(listener)
	err = syscall.Sendmsg(int(socket.Fd()), []byte("ok"), rights, nil, 0)
	syscall.Close(listener)
	socket.Close()
	if err != nil {
		return
	}
	if executable, err := os.Executable(); err == nil {
		syscall.Exec(executable, os.Args, append(os.Environ(), filteredVariable+"=1"))
	}
}

type sockFilter struct {
	code   uint16
	jt, jf uint8
	k      uint32
}

type sockFprog struct {
	len    uint16
	filter *sockFilter
}

// filter is the seccomp program: notify each call in calls; notify calls of
// another architecture or ABI, which the observer cannot decode, and calls
// newer than the table, which it cannot classify; fail io_uring_setup;
// allow the rest.
func filter() []sockFilter {
	const (
		ldAbs  = 0x20 // BPF_LD | BPF_W | BPF_ABS
		jeqK   = 0x15 // BPF_JMP | BPF_JEQ | BPF_K
		jgtK   = 0x25 // BPF_JMP | BPF_JGT | BPF_K
		jgeK   = 0x35 // BPF_JMP | BPF_JGE | BPF_K
		jsetK  = 0x45 // BPF_JMP | BPF_JSET | BPF_K
		retK   = 0x06 // BPF_RET | BPF_K
		offNr  = 0
		offArc = 4
		offArg = 16 // args[0], its low half on a little-endian machine
	)
	program := []sockFilter{
		{code: ldAbs, k: offArc},
		{code: jeqK, jt: 1, k: auditArch},
		{code: retK, k: seccompRetUserNotif},
		{code: ldAbs, k: offNr},
	}
	if checkX32SyscallBits {
		program = append(program,
			sockFilter{code: jgeK, jf: 1, k: x32SyscallBit},
			sockFilter{code: retK, k: seccompRetUserNotif})
	}
	for _, c := range calls {
		switch c.kind {
		case kindRefused:
			program = append(program,
				sockFilter{code: jeqK, jf: 1, k: c.nr},
				sockFilter{code: retK, k: seccompRetErrno | uint32(syscall.ENOSYS)})
		case kindNamespaceClone:
			// Load args[0]; notify if it asks for a namespace; reload the
			// number for the remaining comparisons.
			program = append(program,
				sockFilter{code: jeqK, jf: 5, k: c.nr},
				sockFilter{code: ldAbs, k: offArg},
				sockFilter{code: jsetK, jf: 1, k: namespaceFlags},
				sockFilter{code: retK, k: seccompRetUserNotif},
				sockFilter{code: retK, k: seccompRetAllow},
				sockFilter{code: ldAbs, k: offNr})
		default:
			program = append(program,
				sockFilter{code: jeqK, jf: 1, k: c.nr},
				sockFilter{code: retK, k: seccompRetUserNotif})
		}
	}
	return append(program,
		sockFilter{code: jgtK, jf: 1, k: highestKnownCall},
		sockFilter{code: retK, k: seccompRetUserNotif},
		sockFilter{code: retK, k: seccompRetAllow})
}

// installFilter installs the filter on every thread of this process and
// returns its listener.
func installFilter() (int, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if _, _, errno := syscall.RawSyscall6(syscall.SYS_PRCTL, prSetNoNewPrivs, 1, 0, 0, 0, 0); errno != 0 {
		return -1, fmt.Errorf("prctl(PR_SET_NO_NEW_PRIVS): %w", errno)
	}
	program := filter()
	prog := sockFprog{len: uint16(len(program)), filter: &program[0]}
	flags := uintptr(seccompFlagTsync | seccompFlagTsyncESRCH | seccompFlagNewListener)
	// A thread waiting on the parent then ignores all but fatal signals, so
	// the runtime's preemption signals do not restart every call; kernels
	// before 5.19 lack the flag.
	listener, _, errno := syscall.RawSyscall(sysSeccomp, seccompModeFilter, flags|seccompFlagWaitKillable, uintptr(unsafe.Pointer(&prog)))
	if errno == syscall.EINVAL {
		listener, _, errno = syscall.RawSyscall(sysSeccomp, seccompModeFilter, flags, uintptr(unsafe.Pointer(&prog)))
	}
	runtime.KeepAlive(program)
	if errno != 0 {
		return -1, fmt.Errorf("seccomp(SECCOMP_SET_MODE_FILTER): %w", errno)
	}
	return int(listener), nil
}

// observe runs this test binary again, with the same arguments, as an
// observed child, and returns its exit code and what its process tree
// accessed. An error means the child did not run the tests.
func observe(root string) (int, *recorder, error) {
	executable, err := os.Executable()
	if err != nil {
		return 0, nil, err
	}
	pair, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_SEQPACKET|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return 0, nil, err
	}
	parent, child := os.NewFile(uintptr(pair[0]), "testinputs"), os.NewFile(uintptr(pair[1]), "testinputs")
	defer parent.Close()
	command := exec.Command(executable, os.Args[1:]...)
	command.Args[0] = os.Args[0]
	command.Env = append(os.Environ(), listenerVariable+"=3")
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	command.ExtraFiles = []*os.File{child}
	// The test process dies with this one: cmd/go kills only the process it
	// started when a test times out, and an orphan's filtered calls would
	// fail once the listener closes. The death signal follows the thread
	// that starts the child, so that thread stays locked until the child is
	// reaped.
	command.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	err = command.Start()
	child.Close()
	if err != nil {
		return 0, nil, err
	}
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT, syscall.SIGHUP)
	defer signal.Stop(signals)
	go func() {
		// The tests run in the child: cmd/go's timeout (SIGQUIT) and
		// interrupts go to it.
		for sig := range signals {
			command.Process.Signal(sig)
		}
	}()
	o := newObserver(root)
	o.child, o.executable = command.Process.Pid, executable
	listener, err := receiveListener(parent)
	if err != nil {
		o.rec.fail(err.Error())
	} else {
		o.serve(listener)
	}
	err = command.Wait()
	o.rec.stop()
	if !o.armed.Load() {
		o.rec.refuse("the test process never reported its armed filter")
	}
	state := command.ProcessState
	if state == nil {
		o.rec.refuse(fmt.Sprintf("wait for the test process: %v", err))
		return 1, o.rec, nil
	}
	if status, ok := state.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		fmt.Fprintf(os.Stderr, "testinputs: test process: %v\n", status.Signal())
		return 128 + int(status.Signal()), o.rec, nil
	}
	return state.ExitCode(), o.rec, nil
}

// receiveListener reads the listener the child sends, or the reason it has
// none.
func receiveListener(socket *os.File) (int, error) {
	message := make([]byte, 512)
	control := make([]byte, syscall.CmsgSpace(4))
	n, controlLength, _, _, err := syscall.Recvmsg(int(socket.Fd()), message, control, 0)
	if err != nil {
		return -1, fmt.Errorf("receive the filter listener: %w", err)
	}
	if string(message[:n]) != "ok" {
		if n == 0 {
			return -1, errors.New("the test process sent no filter listener")
		}
		return -1, errors.New("the test process installed no filter: " + string(message[:n]))
	}
	messages, err := syscall.ParseSocketControlMessage(control[:controlLength])
	if err != nil || len(messages) != 1 {
		return -1, errors.New("the test process sent no filter listener")
	}
	fds, err := syscall.ParseUnixRights(&messages[0])
	if err != nil || len(fds) != 1 {
		return -1, errors.New("the test process sent no filter listener")
	}
	return fds[0], nil
}

// An observer serves the listener's notifications.
type observer struct {
	rec   *recorder
	dirs  workdirs
	armed atomic.Bool

	// The child's exec of the test binary from its init (see init) is not
	// the test's: it is skipped once, from the child's main thread, where Go
	// runs initializers.
	child      int
	executable string
	refiltered atomic.Bool
}

func newObserver(root string) *observer {
	return &observer{rec: newRecorder(root), dirs: workdirs{
		processes: map[int]processIDs{},
		places:    map[int][]place{},
	}}
}

// notification is struct seccomp_notif.
type notification struct {
	id    uint64
	pid   uint32
	flags uint32
	nr    int32
	arch  uint32
	ip    uint64
	args  [6]uint64
}

// response is struct seccomp_notif_resp.
type response struct {
	id    uint64
	val   int64
	error int32
	flags uint32
}

// serve starts the workers, each on its own thread: wait for a
// notification, record it, let the call continue. With synchronous wake-up
// (Linux 6.6) the kernel switches between a notifying thread and a waiting
// worker on one CPU, without a trip through the scheduler. The workers
// outlive the test process, continuing the calls of processes it left
// running until this process exits; what they record after stop is ignored.
func (o *observer) serve(listener int) {
	// The flags go by value. Older kernels refuse them and wake through the
	// scheduler.
	syscall.Syscall(syscall.SYS_IOCTL, uintptr(listener), seccompIoctlNotifSetFlags, seccompNotifSyncWakeUp)
	// Eight workers measured fastest on the 16-CPU workbox (2026-10-11,
	// internal/compiler subset: 2 workers 40-47 s, 4 35-42 s, 8 34-39 s,
	// 16 36-64 s, 32 42-44 s, unobserved 25-33 s).
	for range min(runtime.NumCPU(), 8) {
		go func() {
			runtime.LockOSThread()
			buffer := make([]byte, pageSize)
			for {
				var n notification
				if err := ioctl(listener, seccompIoctlNotifRecv, unsafe.Pointer(&n)); err != nil {
					if err == syscall.EINTR || err == syscall.ENOENT {
						continue // interrupted, or the caller died first
					}
					o.rec.fail(fmt.Sprintf("receive a notification: %v", err))
					return
				}
				o.handle(listener, &n, buffer)
				r := response{id: n.id, flags: seccompNotifContinue}
				err := ioctl(listener, seccompIoctlNotifSend, unsafe.Pointer(&r))
				for err == syscall.EINTR {
					err = ioctl(listener, seccompIoctlNotifSend, unsafe.Pointer(&r))
				}
				if err != nil && err != syscall.ENOENT {
					o.rec.fail(fmt.Sprintf("continue a notified call: %v", err))
				}
			}
		}()
	}
}

// handle records one notified call. A read that fails because the thread
// died or was interrupted meanwhile (the notification is no longer valid)
// is dropped: the call made no access, and a restarted call is notified
// again. A read that succeeds is recorded without that check: a thread
// waiting on its notification cannot exit, so its ID names it.
func (o *observer) handle(listener int, n *notification, buffer []byte) {
	tid := int(n.pid)
	if n.arch != auditArch || (checkX32SyscallBits && n.nr&x32SyscallBit != 0) {
		o.rec.fail("a system call of another architecture or ABI")
		return
	}
	c, found := callsByNumber[n.nr]
	if !found {
		if n.nr > highestKnownCall {
			o.rec.fail(fmt.Sprintf("system call %d is newer than the observer's table", n.nr))
		} else {
			o.rec.fail(fmt.Sprintf("unexpected system call %d", n.nr))
		}
		return
	}
	switch c.kind {
	case kindUnobservable, kindNamespaceClone:
		o.rec.fail(c.name)
		return
	case kindFchdir:
		o.dirs.forget(tid)
		return
	case kindSeccomp:
		// seccomp(SECCOMP_SET_MODE_FILTER, flags) with a new listener.
		if n.args[0] == seccompModeFilter && n.args[1]&seccompFlagNewListener != 0 {
			o.rec.fail("a process installed its own seccomp listener")
		}
		return
	case kindClone3:
		// struct clone_args begins with the u64 flags.
		var flags [8]byte
		if got, err := readMemory(tid, n.args[0], flags[:]); err != nil || got < 8 {
			return // the kernel faults the call too
		}
		if *(*uint64)(unsafe.Pointer(&flags[0]))&namespaceFlags != 0 {
			o.rec.fail("clone3 made a namespace")
		}
		return
	}
	a := access{tid: tid, args: n.args, call: c, buffer: buffer}
	first, ok, err := a.path()
	if err != nil {
		if valid(listener, n.id) {
			o.rec.fail(err.Error())
		}
		return
	}
	if ok && first.path == armedPath {
		o.armed.Store(true)
		return
	}
	if c.kind == kindExec && ok && tid == o.child && first.path == o.executable && o.refiltered.CompareAndSwap(false, true) {
		return
	}
	read := true
	switch c.kind {
	case kindOpen:
		read = int(n.args[c.flags])&syscall.O_ACCMODE != syscall.O_WRONLY
	case kindOpenat2:
		var how [8]byte // struct open_how begins with the u64 flags
		if got, err := readMemory(tid, n.args[c.flags], how[:]); err == nil && got == 8 {
			read = int(*(*uint64)(unsafe.Pointer(&how[0])))&syscall.O_ACCMODE != syscall.O_WRONLY
		}
	}
	// The second path of rename and link is decoded even when the first names
	// none: linkat(fd, "", newdirfd, newpath, AT_EMPTY_PATH) still looks the
	// new path up.
	var second []string
	if c.second {
		b := access{tid: tid, args: n.args, call: call{kind: kindStat, dirfd: c.dirfd2, path: c.path2}, buffer: buffer}
		other, ok, err := b.path()
		if err != nil {
			if valid(listener, n.id) {
				o.rec.fail(err.Error())
			}
			return
		}
		if ok {
			second = o.names(tid, other)
		}
	}
	var paths []string
	if ok {
		paths = o.names(tid, first)
	}
	var names []string
	if c.kind == kindExec && ok {
		names, err = a.environment(n.args[c.env])
		if err != nil && valid(listener, n.id) {
			o.rec.fail(err.Error())
			return
		}
	}
	switch c.kind {
	case kindOpen, kindOpenat2:
		for _, path := range paths {
			o.rec.open(path, read)
		}
	case kindStat:
		for _, path := range append(paths, second...) {
			o.rec.stat(path)
		}
	case kindChdir:
		for _, path := range paths {
			o.rec.stat(path)
		}
		if len(paths) > 0 {
			o.dirs.change(tid, paths[len(paths)-1])
		}
	case kindExec:
		for i, path := range paths {
			if i == 0 {
				o.rec.exec(path, names)
			} else {
				o.rec.open(path, true)
			}
		}
	}
}

// names returns the paths under which to record a located path: the path
// with /proc and /dev/fd magic links read, and, for a name relative to a
// working directory the process reached through a link, the same name under
// that link too. The kernel resolved the name against the directory itself,
// which the first path records; the second records the link, so retargeting
// it changes the key.
func (o *observer) names(tid int, l located) []string {
	paths := []string{magic(tid, l.path)}
	if l.base != "" {
		if logical := o.dirs.logical(tid, l.base); logical != "" {
			paths = append(paths, strings.TrimSuffix(logical, "/")+"/"+l.name)
		}
	}
	return paths
}

var callsByNumber = func() map[int32]call {
	byNumber := make(map[int32]call, len(calls))
	for _, c := range calls {
		byNumber[int32(c.nr)] = c
	}
	return byNumber
}()

// valid reports whether a notification's thread still waits on it, so what
// was read from it belongs to that call.
func valid(listener int, id uint64) bool {
	return ioctl(listener, seccompIoctlNotifIDValid, unsafe.Pointer(&id)) == nil
}

// An access is one notified call being decoded.
type access struct {
	tid    int
	args   [6]uint64
	call   call
	buffer []byte
}

var errFault = errors.New("unreadable process memory")

// A located path is the absolute path a call names. A name relative to the
// working directory also keeps that directory, as /proc shows it (its
// physical location), and the name.
type located struct {
	path       string
	base, name string
}

// path returns the absolute path the call names, or false when it names none
// (an empty path that refers to the descriptor itself, or memory or a
// descriptor the kernel rejects too).
func (a *access) path() (located, bool, error) {
	c := a.call
	name, err := a.prefix(a.args[c.path], 0)
	if err != nil {
		if errors.Is(err, errFault) || errors.Is(err, syscall.ESRCH) {
			return located{}, false, nil
		}
		return located{}, false, err
	}
	dirfd := atFdcwd
	if c.dirfd >= 0 {
		dirfd = int(int32(a.args[c.dirfd]))
	}
	inRoot := false
	if c.kind == kindOpenat2 {
		var how [24]byte
		if got, err := readMemory(a.tid, a.args[c.flags], how[:]); err == nil && got == 24 {
			inRoot = *(*uint64)(unsafe.Pointer(&how[16]))&resolveInRoot != 0
		}
	}
	if name == "" {
		if c.kind != kindExec || c.atFlags == 0 || a.args[c.atFlags]&atEmptyPath == 0 || dirfd == atFdcwd {
			return located{}, false, nil // the descriptor's file was opened by path earlier
		}
		path, ok, err := a.link("fd/" + strconv.Itoa(dirfd))
		return located{path: path}, ok, err
	}
	if name[0] == '/' && !inRoot {
		return located{path: name}, true, nil
	}
	link := "cwd"
	if dirfd != atFdcwd {
		link = "fd/" + strconv.Itoa(dirfd)
	}
	base, ok, err := a.link(link)
	if !ok || err != nil {
		return located{}, false, err
	}
	l := located{path: strings.TrimSuffix(base, "/") + "/" + strings.TrimPrefix(name, "/")}
	if dirfd == atFdcwd {
		l.base, l.name = base, name
	}
	return l, true, nil
}

// link reads the thread's working directory or one of its descriptors.
func (a *access) link(name string) (string, bool, error) {
	target, err := os.Readlink("/proc/" + strconv.Itoa(a.tid) + "/" + name)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ESRCH) {
			return "", false, nil // a closed descriptor or a dead thread: the call fails too
		}
		return "", false, err
	}
	if !strings.HasPrefix(target, "/") {
		return "", false, nil // a pipe, socket or anonymous file: the call fails with ENOTDIR
	}
	return undeleted(target), true, nil
}

// undeleted removes the " (deleted)" the kernel appends to the target of a
// /proc link whose file was removed, unless a file of that very name exists.
func undeleted(target string) string {
	stripped, ok := strings.CutSuffix(target, " (deleted)")
	if !ok {
		return target
	}
	if _, err := os.Lstat(target); err == nil {
		return target
	}
	return stripped
}

// magic returns the file a path through a /proc magic link names: a
// process's working directory, root or descriptor (/proc/self/cwd/...,
// /proc/<pid>/fd/<n>/..., /proc/thread-self/root/..., /dev/fd/<n>/...,
// /dev/stdin). The kernel follows such a link to the file itself, not to
// the text the link shows, so the recorder skips /proc and /dev; read here,
// the path is recorded like any other. The links of "self" are the calling
// thread's. Any other path is returned unchanged.
func magic(tid int, path string) string {
	rest, ok := strings.CutPrefix(path, "/dev/")
	if ok {
		switch {
		case rest == "stdin", rest == "stdout", rest == "stderr":
			rest = "self/fd/" + strconv.Itoa(slices.Index([]string{"stdin", "stdout", "stderr"}, rest))
		case strings.HasPrefix(rest, "fd/"):
			rest = "self/" + rest
		default:
			return path
		}
	} else if rest, ok = strings.CutPrefix(path, "/proc/"); !ok {
		return path
	}
	process, rest, _ := strings.Cut(rest, "/")
	switch {
	case process == "self" || process == "thread-self":
		process = strconv.Itoa(tid)
	case strings.Trim(process, "0123456789") != "" || process == "":
		return path
	}
	if task, ok := strings.CutPrefix(rest, "task/"); ok {
		process, rest, _ = strings.Cut(task, "/")
		if strings.Trim(process, "0123456789") != "" || process == "" {
			return path
		}
	}
	link, rest, _ := strings.Cut(rest, "/")
	if link == "fd" {
		var fd string
		fd, rest, _ = strings.Cut(rest, "/")
		if strings.Trim(fd, "0123456789") != "" || fd == "" {
			return path
		}
		link = "fd/" + fd
	} else if link != "cwd" && link != "root" {
		return path
	}
	if rest == "" {
		// The link itself: reading it looks nothing up, and reopening a
		// descriptor reads a file whose open was recorded by its path.
		return path
	}
	target, err := os.Readlink("/proc/" + process + "/" + link)
	if err != nil || !strings.HasPrefix(target, "/") {
		return path // a dead process, a closed descriptor or a pipe: nothing to record
	}
	return strings.TrimSuffix(undeleted(target), "/") + "/" + rest
}

// environment returns the name of every variable in an execve environment.
func (a *access) environment(envp uint64) ([]string, error) {
	var names []string
	var pointer [8]byte
	for i := uint64(0); ; i++ {
		if got, err := readMemory(a.tid, envp+8*i, pointer[:]); err != nil || got < 8 {
			if err != nil && !errors.Is(err, errFault) && !errors.Is(err, syscall.ESRCH) {
				return nil, err
			}
			return names, nil
		}
		address := *(*uint64)(unsafe.Pointer(&pointer[0]))
		if address == 0 {
			return names, nil
		}
		name, err := a.prefix(address, '=')
		if err != nil {
			if errors.Is(err, errFault) || errors.Is(err, syscall.ESRCH) {
				return names, nil
			}
			return nil, err
		}
		names = append(names, name)
	}
}

// prefix reads a NUL-terminated string, up to its first stop byte if stop is
// not 0, of at most pathMax bytes.
func (a *access) prefix(address uint64, stop byte) (string, error) {
	var out []byte
	for len(out) <= pathMax {
		chunk := a.buffer[:pageSize-address%pageSize]
		n, err := readMemory(a.tid, address, chunk)
		if err != nil {
			return "", err
		}
		if n == 0 {
			return "", errFault
		}
		chunk = chunk[:n]
		end := bytes.IndexByte(chunk, 0)
		if stop != 0 {
			if j := bytes.IndexByte(chunk, stop); j >= 0 && (end < 0 || j < end) {
				end = j
			}
		}
		if end >= 0 {
			return string(append(out, chunk[:end]...)), nil
		}
		out = append(out, chunk...)
		address += uint64(n)
	}
	return "", errFault // longer than any path the kernel accepts
}

// remoteIovec is struct iovec naming memory in another process.
type remoteIovec struct {
	base   uintptr
	length uint64
}

// readMemory copies another process's memory with process_vm_readv.
func readMemory(tid int, address uint64, into []byte) (int, error) {
	local := syscall.Iovec{Base: &into[0], Len: uint64(len(into))}
	remote := remoteIovec{base: uintptr(address), length: uint64(len(into))}
	n, _, errno := syscall.Syscall6(sysProcessVMReadv, uintptr(tid), uintptr(unsafe.Pointer(&local)), 1, uintptr(unsafe.Pointer(&remote)), 1, 0)
	switch errno {
	case 0:
		return int(n), nil
	case syscall.EFAULT:
		return 0, errFault
	default:
		return 0, errno
	}
}

func ioctl(fd int, request uintptr, argument unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), request, uintptr(argument)); errno != 0 {
		return errno
	}
	return nil
}

// writable reports whether this process may write the directory.
func writable(dir string) bool { return syscall.Access(dir, 2) == nil } // W_OK

// foreignDir reports whether a path names a directory, not a link, that
// another user owns.
func foreignDir(dir string) bool {
	var info syscall.Stat_t
	return syscall.Lstat(dir, &info) == nil && info.Mode&syscall.S_IFMT == syscall.S_IFDIR && int(info.Uid) != os.Geteuid()
}

// physicalDir returns where an existing path leads, links and ".." resolved,
// for the price of an open and a readlink.
func physicalDir(dir string) (string, bool) {
	const oPath = 0x200000 // O_PATH on amd64 and arm64; package syscall lacks it
	fd, err := syscall.Open(dir, oPath|syscall.O_CLOEXEC, 0)
	if err != nil {
		return "", false
	}
	physical, err := os.Readlink("/proc/self/fd/" + strconv.Itoa(fd))
	syscall.Close(fd)
	return physical, err == nil
}

// privileged reports whether this process, and so the tree it observes, may
// write or chmod a directory regardless of its owner and mode: it runs as
// root or holds CAP_CHOWN, CAP_DAC_OVERRIDE or CAP_FOWNER (bits 0, 1 and 3)
// in its permitted or effective set. An unreadable status counts as
// privileged.
func privileged() bool {
	if os.Geteuid() == 0 {
		return true
	}
	status, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return true
	}
	seen := 0
	for line := range strings.SplitSeq(string(status), "\n") {
		name, value, ok := strings.Cut(line, ":")
		if !ok || (name != "CapPrm" && name != "CapEff") {
			continue
		}
		caps, err := strconv.ParseUint(strings.TrimSpace(value), 16, 64)
		if err != nil || caps&(1<<0|1<<1|1<<3) != 0 {
			return true
		}
		seen++
	}
	return seen != 2
}

// workdirs follows the working directories processes changed to through a
// link. /proc shows a working directory by its physical location, so a name
// relative to it would be recorded there, and retargeting the link would go
// unseen. A chdir is notified before it runs, and may fail; the directory
// it names is remembered with its physical location, and a later relative
// name uses it only while /proc still shows that location. A working
// directory is shared by a thread group and inherited across fork and exec,
// so the places are kept per thread group, and a group with none takes its
// parent's. A chdir that crosses no link, and every fchdir, leaves a process
// at its physical location, which needs no place.
type workdirs struct {
	active atomic.Bool // a place was ever remembered; until then nothing reads /proc

	mu        sync.Mutex
	processes map[int]processIDs // thread → its group and the group's parent
	places    map[int][]place    // thread group → recent places, newest first; empty after fchdir
}

type processIDs struct{ group, parent int }

// A place is a working directory by the name a chdir gave and by its
// physical location.
type place struct{ logical, physical string }

// maxPlaces bounds the places kept per thread group: a child inherits the
// working directory its parent had at the fork, which may be one of the
// parent's earlier places.
const maxPlaces = 4

// change remembers a chdir to a directory named through a link.
func (w *workdirs) change(tid int, logical string) {
	for _, prefix := range []string{"/proc/", "/dev/"} {
		if strings.HasPrefix(logical, prefix) {
			return // still a magic link, which would resolve in this process
		}
	}
	physical, err := filepath.EvalSymlinks(logical)
	if err != nil || physical == clean(logical) {
		return // the chdir fails, or crosses no link
	}
	ids, ok := w.ids(tid)
	if !ok {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	places, found := w.places[ids.group]
	if !found {
		places = w.inheritLocked(ids)
	}
	places = append([]place{{logical, physical}}, places...)
	w.places[ids.group] = places[:min(len(places), maxPlaces)]
	w.active.Store(true)
}

// forget records an fchdir: the process is now at a physical location.
func (w *workdirs) forget(tid int) {
	if !w.active.Load() {
		return
	}
	ids, ok := w.ids(tid)
	if !ok {
		return
	}
	w.mu.Lock()
	w.places[ids.group] = []place{}
	w.mu.Unlock()
}

// logical returns the name of a thread's working directory if it reached
// its physical location, which /proc shows, through a link; otherwise "".
func (w *workdirs) logical(tid int, physical string) string {
	if !w.active.Load() {
		return ""
	}
	ids, ok := w.ids(tid)
	if !ok {
		return ""
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	places, found := w.places[ids.group]
	if !found {
		places = w.inheritLocked(ids)
		w.places[ids.group] = places
	}
	for _, p := range places {
		if p.physical == physical {
			return p.logical
		}
	}
	return ""
}

// inheritLocked returns the places of the nearest ancestor that has any. It
// reads /proc only for processes not yet seen, and only while w.mu is held,
// which serializes these rare reads.
func (w *workdirs) inheritLocked(ids processIDs) []place {
	for range 16 {
		if ids.parent <= 1 {
			break
		}
		if places, ok := w.places[ids.parent]; ok {
			return slices.Clone(places)
		}
		next, ok := w.processes[ids.parent]
		if !ok {
			if next, ok = readProcessIDs(ids.parent); !ok {
				break
			}
			w.processes[ids.parent] = next
		}
		ids = next
	}
	return []place{}
}

// ids returns a thread's group and the group's parent.
func (w *workdirs) ids(tid int) (processIDs, bool) {
	w.mu.Lock()
	ids, ok := w.processes[tid]
	w.mu.Unlock()
	if ok {
		return ids, true
	}
	if ids, ok = readProcessIDs(tid); !ok {
		return ids, false
	}
	w.mu.Lock()
	w.processes[tid] = ids
	w.mu.Unlock()
	return ids, true
}

// readProcessIDs reads a thread's group and the group's parent from /proc.
func readProcessIDs(tid int) (processIDs, bool) {
	status, err := os.ReadFile("/proc/" + strconv.Itoa(tid) + "/status")
	if err != nil {
		return processIDs{}, false
	}
	var ids processIDs
	seen := 0
	for line := range strings.SplitSeq(string(status), "\n") {
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		var into *int
		switch name {
		case "Tgid":
			into = &ids.group
		case "PPid":
			into = &ids.parent
		default:
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return processIDs{}, false
		}
		*into = n
		if seen++; seen == 2 {
			return ids, true
		}
	}
	return processIDs{}, false
}
