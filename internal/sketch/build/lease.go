package build

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// A private root (Cache.private) belongs to one process. The process holds a
// shared flock lease on the root's lease file from the moment it takes the
// root until it exits, so the root survives its builds and the links that
// read the module afterwards; the kernel releases the lease when the process
// dies, however it dies. A sweep removes a root only when the root is older
// than privateLifetime and the sweep takes the exclusive lock without
// waiting: then no live process owns it. A timestamp alone never removes a
// root.
//
// The sweep renames the root aside while it holds the exclusive lock and
// removes it there. An owner that opened the old lease file waits for its
// shared lock, then sees that the root's lease path no longer names that
// file and takes the root again, fresh; an owner that arrives after the
// rename makes a new root. So an owner never holds a lease on a root that is
// being removed.
//
// Prior art: Go's build cache locks its entries with cmd/go/internal/
// lockedfile, flock on Unix; Cargo locks its package cache with a file lock;
// ccache keeps lock files with stale-owner detection.

// leaseName is the lease file inside a private root, beside its modules.
const leaseName = "lease"

// removingPrefix names a root a sweep has renamed aside to remove.
const removingPrefix = ".removing-"

// leases holds this process's lease file of each private root it owns, open
// until the process exits.
var leases struct {
	sync.Mutex
	held map[string]*os.File
}

// holdLease makes the process the owner of root for the rest of its life: it
// takes root's lease once and keeps it.
func holdLease(root string) error {
	leases.Lock()
	defer leases.Unlock()
	if leases.held[root] != nil {
		return nil
	}
	f, err := takeLease(root)
	if err != nil {
		return err
	}
	if leases.held == nil {
		leases.held = map[string]*os.File{}
	}
	leases.held[root] = f
	return nil
}

// heldLease reports whether this process owns root.
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

// sweep removes the private roots under c that no live process owns and no
// process has taken for privateLifetime, and what earlier sweeps left
// aside. It never waits for an owner, and a root it cannot remove stays for
// a later sweep.
func (c Cache) sweep(now time.Time) {
	if !leasesSupported {
		return
	}
	parent := filepath.Join(c.Root, "private")
	list, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	for _, e := range list {
		root := filepath.Join(parent, e.Name())
		if strings.HasPrefix(e.Name(), removingPrefix) {
			_ = os.RemoveAll(root)
			continue
		}
		if heldLease(root) {
			continue
		}
		info, err := os.Lstat(root)
		if err != nil || !info.IsDir() || now.Sub(info.ModTime()) <= privateLifetime {
			continue
		}
		collect(root)
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
		return false // a live owner holds it, or another sweep took it
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return false
	}
	aside := filepath.Join(filepath.Dir(root), removingPrefix+filepath.Base(root)+"-"+hex.EncodeToString(nonce[:]))
	if err := os.Rename(root, aside); err != nil {
		return false
	}
	_ = os.RemoveAll(aside)
	return true
}
