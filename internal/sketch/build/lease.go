package build

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A private root (Cache.private) belongs to the builds of one process. A
// build holds a shared flock lease on the root's lease file from the moment
// it takes the root until its link ends (Module.Release); the kernel
// releases the lease when the process dies, however it dies. Builds of one
// process share one lease file and count their uses; the last release
// closes it, so a long-lived host holds nothing between builds. A sweep
// removes a root only when the root is older than privateLifetime and the
// sweep takes the exclusive lock without waiting: then no build holds it. A
// timestamp alone never removes a root.
//
// The sweep renames the root aside while it holds the exclusive lock, then
// unlinks the lease and removes the rest there. A build that opened the old
// lease file waits for its shared lock, then sees that the root's lease path
// no longer names that file and takes the root again, fresh; a build that
// arrives after the rename makes a new root. So a build never holds a lease
// on a root that is being removed, nor reuses a module inside one.
//
// Sweeps are rate-limited like Go's cache trim: a stamp file in Root/private
// records when the last complete sweep ended, a sweep starts at most once
// per sweepInterval, one process claims each pass by locking the stamp, and
// each pass is bounded. A pass that hits a bound leaves the sweep due, with
// a cursor, so the next build continues it until it completes. A build that
// finds the stamp recent reads only the stamp.
//
// Prior art: Go's build cache locks its entries with cmd/go/internal/
// lockedfile, flock on Unix, and trims at most daily behind trim.txt; Cargo
// locks its package cache with a file lock; ccache keeps lock files with
// stale-owner detection.

// leaseName is the lease file inside a private root, beside its modules.
const leaseName = "lease"

// removingPrefix names a root a sweep has renamed aside to remove.
const removingPrefix = ".removing-"

// stampName is the sweep stamp in Root/private (type stamp). Its lock
// claims a pass.
const stampName = ".swept"

// sweepInterval is the least time between the end of one complete sweep and
// the start of the next.
const sweepInterval = 24 * time.Hour

// A sweep pass reads names of Root/private sweepChunk at a time, examines at
// most sweepEntries of them and removes at most sweepRemovals roots; a pass
// that hits a bound leaves the rest to the next build.
const (
	sweepChunk    = 256
	sweepEntries  = 1024
	sweepRemovals = 64
)

// testHookCollecting, when set, runs after a sweep has renamed a root aside
// and unlinked its lease, before it removes the rest. Tests use it to
// let a build arrive in that window.
var testHookCollecting func(root string)

// lease is the open lease file of one private root and the number of
// builds of this process that hold it.
type lease struct {
	file  *os.File
	users int
}

// leases holds this process's lease of each private root some build holds.
var leases struct {
	sync.Mutex
	held map[string]*lease
}

// acquireLease takes one use of root's lease for a build and returns its
// release, which is safe to call more than once. The first use takes the
// lease (takeLease); later uses share it until the last one is released.
func acquireLease(root string) (func(), error) {
	leases.Lock()
	defer leases.Unlock()
	l := leases.held[root]
	if l == nil {
		f, err := takeLease(root)
		if err != nil {
			return nil, err
		}
		l = &lease{file: f}
		if leases.held == nil {
			leases.held = map[string]*lease{}
		}
		leases.held[root] = l
	}
	l.users++
	var once sync.Once
	return func() { once.Do(func() { releaseLease(root, l) }) }, nil
}

// releaseLease gives back one use of l; the last use closes the file, which
// releases the lock.
func releaseLease(root string, l *lease) {
	leases.Lock()
	defer leases.Unlock()
	l.users--
	if l.users == 0 {
		delete(leases.held, root)
		l.file.Close()
	}
}

// heldLease reports whether a build of this process holds root.
func heldLease(root string) bool {
	leases.Lock()
	defer leases.Unlock()
	return leases.held[root] != nil
}

// takeLease makes root, if a sweep has not left it, and returns its lease
// file with a shared lock on it. Closing the file releases the lease.
func takeLease(root string) (*os.File, error) {
	path := filepath.Join(root, leaseName)
	for range 16 {
		if err := os.MkdirAll(root, 0o755); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
		if errors.Is(err, fs.ErrNotExist) {
			continue // a sweep moved the root between the two calls
		}
		if err != nil {
			return nil, err
		}
		if err := lockShared(f); err != nil {
			f.Close()
			return nil, err
		}
		if sameLease(f, path) {
			return f, nil
		}
		f.Close() // a sweep removed the root while this waited
	}
	return nil, fmt.Errorf("build: private root %s keeps being removed", root)
}

// sameLease reports whether path still names the open lease file f.
func sameLease(f *os.File, path string) bool {
	open, err := f.Stat()
	if err != nil {
		return false
	}
	named, err := os.Stat(path)
	return err == nil && os.SameFile(open, named)
}

// maybeSweep runs a sweep pass of c unless the last complete sweep is less
// than sweepInterval old, or another process holds the claim. A recent
// stamp costs one file read and no directory scan; a cache without private
// roots is not touched. The claim is the stamp's lock, which the kernel
// drops if the claimant dies. The stamp records a sweep as complete only
// after a pass that reached the end of the directory without hitting a
// bound; a pass that hits one, or dies, leaves the sweep due, so the next
// build continues from the recorded cursor. Every pass stays bounded, and
// reclaim keeps pace with however many roots builds create.
func (c Cache) maybeSweep(now time.Time) {
	if !leasesSupported {
		return
	}
	path := filepath.Join(c.Root, "private", stampName)
	if data, err := os.ReadFile(path); err == nil && readStamp(data).recent(now) {
		return
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return // no private roots at all, or no access: nothing to do
	}
	defer f.Close()
	if !tryLockExclusive(f) {
		return // another process is sweeping
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return
	}
	s := readStamp(data)
	if s.recent(now) {
		return // a sweep completed since the first read
	}
	complete, cursor := c.sweep(now, s.cursor)
	if complete {
		s = stamp{completed: now.Unix()}
	} else {
		s.cursor = cursor
	}
	if f.Truncate(0) == nil {
		_, _ = f.WriteAt(s.bytes(), 0)
	}
}

// stamp is the content of the stamp file: the Unix time, in seconds, at
// which the last complete sweep ended, and the cursor at which an unfinished
// sweep continues (names of Root/private to pass over), as "SECONDS CURSOR".
type stamp struct {
	completed int64
	cursor    int
}

// readStamp parses a stamp; a torn or foreign one is a sweep that never
// completed, due at once from the start.
func readStamp(data []byte) stamp {
	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		return stamp{}
	}
	completed, err1 := strconv.ParseInt(fields[0], 10, 64)
	cursor, err2 := strconv.Atoi(fields[1])
	if err1 != nil || err2 != nil || cursor < 0 {
		return stamp{}
	}
	return stamp{completed: completed, cursor: cursor}
}

func (s stamp) bytes() []byte {
	return []byte(strconv.FormatInt(s.completed, 10) + " " + strconv.Itoa(s.cursor))
}

// recent reports whether the last complete sweep ended less than
// sweepInterval before now.
func (s stamp) recent(now time.Time) bool {
	if s.completed == 0 {
		return false
	}
	age := now.Sub(time.Unix(s.completed, 0))
	return age >= 0 && age < sweepInterval
}

// sweep is one bounded pass from cursor: it removes the private roots under
// c that no build holds and no build has taken for privateLifetime, and what
// earlier passes left aside. It reads names in chunks of sweepChunk, passes
// over the first cursor names, which earlier passes of this sweep examined,
// then examines at most sweepEntries names and removes at most sweepRemovals
// roots. It reports whether the pass reached the end of the directory, and
// otherwise the cursor at which the next pass continues: the names it passed
// over and examined, less those it removed. Passing over a name costs only
// its read, never a stat. Roots created during a sweep may shift the order,
// so one sweep can miss a root or examine one twice; the next sweep reaches
// it. It never waits for an owner, and a root it cannot remove stays for a
// later pass.
func (c Cache) sweep(now time.Time, cursor int) (bool, int) {
	if !leasesSupported {
		return true, 0
	}
	parent := filepath.Join(c.Root, "private")
	dir, err := os.Open(parent)
	if err != nil {
		return true, 0
	}
	defer dir.Close()
	passed, examined, removed := 0, 0, 0
	for {
		names, err := dir.Readdirnames(sweepChunk)
		for _, name := range names {
			if passed < cursor {
				passed++
				continue
			}
			if examined == sweepEntries || removed == sweepRemovals {
				return false, cursor + examined - removed
			}
			examined++
			if c.reclaim(now, filepath.Join(parent, name)) {
				removed++
			}
		}
		if err != nil || len(names) == 0 {
			return true, 0
		}
	}
}

// reclaim removes one entry of Root/private if a sweep may: what an earlier
// pass left aside, or a root no build holds that is older than
// privateLifetime. It reports whether it removed it.
func (c Cache) reclaim(now time.Time, root string) bool {
	if strings.HasPrefix(filepath.Base(root), removingPrefix) {
		_ = os.RemoveAll(root)
		return true
	}
	if heldLease(root) {
		return false
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || now.Sub(info.ModTime()) <= privateLifetime {
		return false
	}
	return collect(root)
}

// collect removes root if it takes root's lease exclusively without
// waiting, and reports whether it removed it.
func collect(root string) bool {
	path := filepath.Join(root, leaseName)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return false
	}
	if !tryLockExclusive(f) || !sameLease(f, path) {
		f.Close()
		return false // a live build holds it, or another sweep took it
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		f.Close()
		return false
	}
	aside := filepath.Join(filepath.Dir(root), removingPrefix+filepath.Base(root)+"-"+hex.EncodeToString(nonce[:]))
	if err := os.Rename(root, aside); err != nil {
		f.Close()
		return false
	}
	// The lease goes first: from here no build can lock this root's lease
	// by name, and what remains is a tree no build reaches. So the lock is
	// released before the removal: a build that waits on the old lease file
	// wakes, finds that the root's path no longer names it, and takes a
	// fresh root without waiting for the tree to be removed.
	_ = os.Remove(filepath.Join(aside, leaseName))
	f.Close()
	if testHookCollecting != nil {
		testHookCollecting(root)
	}
	_ = os.RemoveAll(aside)
	return true
}
