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
// records the last pass, a pass runs at most once per sweepInterval, one
// process claims it by locking the stamp, and each pass is bounded. A build
// that finds the stamp recent reads only the stamp.
//
// Prior art: Go's build cache locks its entries with cmd/go/internal/
// lockedfile, flock on Unix, and trims at most daily behind trim.txt; Cargo
// locks its package cache with a file lock; ccache keeps lock files with
// stale-owner detection.

// leaseName is the lease file inside a private root, beside its modules.
const leaseName = "lease"

// removingPrefix names a root a sweep has renamed aside to remove.
const removingPrefix = ".removing-"

// stampName is the sweep stamp in Root/private: the Unix time, in seconds,
// at which the last pass was claimed. Its lock claims a pass.
const stampName = ".swept"

// sweepInterval is the least time between two sweep passes of one cache.
const sweepInterval = 24 * time.Hour

// A sweep pass reads at most sweepEntries names of Root/private and removes
// at most sweepRemovals roots; the next pass continues.
const (
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

// maybeSweep runs a sweep pass of c when the last one is sweepInterval old
// and no other process is running one. A recent stamp costs one file read
// and no directory scan; a cache without private roots is not touched.
func (c Cache) maybeSweep(now time.Time) {
	if !leasesSupported {
		return
	}
	stamp := filepath.Join(c.Root, "private", stampName)
	if data, err := os.ReadFile(stamp); err == nil && recent(data, now) {
		return
	}
	f, err := os.OpenFile(stamp, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return // no private roots at all, or no access: nothing to do
	}
	defer f.Close()
	if !tryLockExclusive(f) {
		return // another process is sweeping
	}
	if data, err := io.ReadAll(f); err != nil || recent(data, now) {
		return // a pass ran since the first read
	}
	if f.Truncate(0) != nil {
		return
	}
	if _, err := f.WriteAt([]byte(strconv.FormatInt(now.Unix(), 10)), 0); err != nil {
		return
	}
	c.sweep(now)
}

// recent reports whether stamp names a pass less than sweepInterval before
// now. A torn or foreign stamp is not recent.
func recent(stamp []byte, now time.Time) bool {
	seconds, err := strconv.ParseInt(strings.TrimSpace(string(stamp)), 10, 64)
	if err != nil {
		return false
	}
	age := now.Sub(time.Unix(seconds, 0))
	return age >= 0 && age < sweepInterval
}

// sweep is one bounded pass: it removes the private roots under c that no
// build holds and no build has taken for privateLifetime, and what earlier
// passes left aside. It never waits for an owner, and a root it cannot
// remove stays for a later pass.
func (c Cache) sweep(now time.Time) {
	if !leasesSupported {
		return
	}
	parent := filepath.Join(c.Root, "private")
	dir, err := os.Open(parent)
	if err != nil {
		return
	}
	list, _ := dir.ReadDir(sweepEntries)
	dir.Close()
	removed := 0
	for _, e := range list {
		if removed == sweepRemovals {
			return
		}
		root := filepath.Join(parent, e.Name())
		if strings.HasPrefix(e.Name(), removingPrefix) {
			_ = os.RemoveAll(root)
			removed++
			continue
		}
		if !e.IsDir() || heldLease(root) {
			continue
		}
		info, err := os.Lstat(root)
		if err != nil || !info.IsDir() || now.Sub(info.ModTime()) <= privateLifetime {
			continue
		}
		if collect(root) {
			removed++
		}
	}
}

// collect removes root if it takes root's lease exclusively without
// waiting, and reports whether it removed it.
func collect(root string) bool {
	path := filepath.Join(root, leaseName)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return false
	}
	defer f.Close()
	if !tryLockExclusive(f) || !sameLease(f, path) {
		return false // a live build holds it, or another sweep took it
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return false
	}
	aside := filepath.Join(filepath.Dir(root), removingPrefix+filepath.Base(root)+"-"+hex.EncodeToString(nonce[:]))
	if err := os.Rename(root, aside); err != nil {
		return false
	}
	// The lease goes first: from here no build can lock this root's lease
	// by name, and what remains is a tree no build reaches.
	_ = os.Remove(filepath.Join(aside, leaseName))
	if testHookCollecting != nil {
		testHookCollecting(root)
	}
	_ = os.RemoveAll(aside)
	return true
}
