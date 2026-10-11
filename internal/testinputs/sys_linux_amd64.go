package testinputs

const (
	auditArch           = 0xc000003e // AUDIT_ARCH_X86_64
	sysSeccomp          = 317
	sysProcessVMReadv   = 310
	x32SyscallBit       = 0x40000000
	checkX32SyscallBits = true
	// highestKnownCall is the last system call of Linux 6.12 (mseal). A
	// higher number is a call this table cannot classify, so it notifies and
	// fails closed.
	highestKnownCall = 462
)

// calls are the system calls the filter notifies about, by number. A path
// argument is read from the waiting thread; see call for the fields.
var calls = []call{
	{nr: 2, kind: kindOpen, dirfd: -1, path: 0, flags: 1},     // open
	{nr: 85, kind: kindStat, dirfd: -1, path: 0},              // creat: a new file written, so only its existence
	{nr: 257, kind: kindOpen, dirfd: 0, path: 1, flags: 2},    // openat
	{nr: 437, kind: kindOpenat2, dirfd: 0, path: 1, flags: 2}, // openat2
	{nr: 4, kind: kindStat, dirfd: -1, path: 0},               // stat
	{nr: 6, kind: kindStat, dirfd: -1, path: 0},               // lstat
	{nr: 262, kind: kindStat, dirfd: 0, path: 1, atFlags: 3},  // newfstatat
	{nr: 332, kind: kindStat, dirfd: 0, path: 1, atFlags: 2},  // statx
	{nr: 21, kind: kindStat, dirfd: -1, path: 0},              // access
	{nr: 269, kind: kindStat, dirfd: 0, path: 1},              // faccessat
	{nr: 439, kind: kindStat, dirfd: 0, path: 1, atFlags: 3},  // faccessat2
	{nr: 89, kind: kindStat, dirfd: -1, path: 0},              // readlink
	{nr: 267, kind: kindStat, dirfd: 0, path: 1},              // readlinkat
	{nr: 191, kind: kindStat, dirfd: -1, path: 0},             // getxattr
	{nr: 192, kind: kindStat, dirfd: -1, path: 0},             // lgetxattr
	{nr: 194, kind: kindStat, dirfd: -1, path: 0},             // listxattr
	{nr: 195, kind: kindStat, dirfd: -1, path: 0},             // llistxattr
	{nr: 137, kind: kindStat, dirfd: -1, path: 0},             // statfs
	{nr: 303, kind: kindStat, dirfd: 0, path: 1, atFlags: 4},  // name_to_handle_at
	{nr: 254, kind: kindStat, dirfd: -1, path: 1},             // inotify_add_watch
	// Writes whose outcome depends on what exists, recorded as lookups.
	{nr: 83, kind: kindStat, dirfd: -1, path: 0},                                     // mkdir
	{nr: 84, kind: kindStat, dirfd: -1, path: 0},                                     // rmdir
	{nr: 87, kind: kindStat, dirfd: -1, path: 0},                                     // unlink
	{nr: 82, kind: kindStat, dirfd: -1, path: 0, second: true, dirfd2: -1, path2: 1}, // rename
	{nr: 86, kind: kindStat, dirfd: -1, path: 0, second: true, dirfd2: -1, path2: 1}, // link
	{nr: 88, kind: kindStat, dirfd: -1, path: 1},                                     // symlink: the link's path
	{nr: 90, kind: kindStat, dirfd: -1, path: 0},                                     // chmod
	{nr: 92, kind: kindStat, dirfd: -1, path: 0},                                     // chown
	{nr: 94, kind: kindStat, dirfd: -1, path: 0},                                     // lchown
	{nr: 76, kind: kindStat, dirfd: -1, path: 0},                                     // truncate
	{nr: 132, kind: kindStat, dirfd: -1, path: 0},                                    // utime
	{nr: 235, kind: kindStat, dirfd: -1, path: 0},                                    // utimes
	{nr: 133, kind: kindStat, dirfd: -1, path: 0},                                    // mknod
	{nr: 258, kind: kindStat, dirfd: 0, path: 1},                                     // mkdirat
	{nr: 259, kind: kindStat, dirfd: 0, path: 1},                                     // mknodat
	{nr: 260, kind: kindStat, dirfd: 0, path: 1},                                     // fchownat
	{nr: 261, kind: kindStat, dirfd: 0, path: 1},                                     // futimesat
	{nr: 263, kind: kindStat, dirfd: 0, path: 1},                                     // unlinkat
	{nr: 264, kind: kindStat, dirfd: 0, path: 1, second: true, dirfd2: 2, path2: 3},  // renameat
	{nr: 265, kind: kindStat, dirfd: 0, path: 1, second: true, dirfd2: 2, path2: 3},  // linkat
	{nr: 266, kind: kindStat, dirfd: 1, path: 2},                                     // symlinkat: the link's path
	{nr: 268, kind: kindStat, dirfd: 0, path: 1},                                     // fchmodat
	{nr: 280, kind: kindStat, dirfd: 0, path: 1},                                     // utimensat
	{nr: 316, kind: kindStat, dirfd: 0, path: 1, second: true, dirfd2: 2, path2: 3},  // renameat2
	{nr: 452, kind: kindStat, dirfd: 0, path: 1},                                     // fchmodat2
	{nr: 188, kind: kindStat, dirfd: -1, path: 0},                                    // setxattr
	{nr: 189, kind: kindStat, dirfd: -1, path: 0},                                    // lsetxattr
	{nr: 197, kind: kindStat, dirfd: -1, path: 0},                                    // removexattr
	{nr: 198, kind: kindStat, dirfd: -1, path: 0},                                    // lremovexattr
	{nr: 80, kind: kindChdir, dirfd: -1, path: 0},                                    // chdir
	{nr: 59, kind: kindExec, dirfd: -1, path: 0, env: 2},                             // execve
	{nr: 322, kind: kindExec, dirfd: 0, path: 1, env: 3, atFlags: 4},                 // execveat
	{nr: 435, kind: kindClone3},                                                      // clone3: its flags are in memory
	{nr: 317, kind: kindSeccomp},                                                     // a listener installed below would take calls from this one
	{nr: 161, kind: kindUnobservable, name: "chroot"},                                // paths stop naming what the observer sees
	{nr: 155, kind: kindUnobservable, name: "pivot_root"},
	{nr: 272, kind: kindUnobservable, name: "unshare"},
	{nr: 308, kind: kindUnobservable, name: "setns"},
	{nr: 165, kind: kindUnobservable, name: "mount"},
	{nr: 166, kind: kindUnobservable, name: "umount2"},
	{nr: 428, kind: kindUnobservable, name: "open_tree"}, // the new mount API
	{nr: 429, kind: kindUnobservable, name: "move_mount"},
	{nr: 430, kind: kindUnobservable, name: "fsopen"},
	{nr: 432, kind: kindUnobservable, name: "fsmount"},
	{nr: 433, kind: kindUnobservable, name: "fspick"},
	{nr: 442, kind: kindUnobservable, name: "mount_setattr"},
	{nr: 304, kind: kindUnobservable, name: "open_by_handle_at"}, // opens a file no path names
	{nr: 134, kind: kindUnobservable, name: "uselib"},
	{nr: 425, kind: kindRefused, name: "io_uring_setup"}, // a ring opens files without system calls
	{nr: 56, kind: kindNamespaceClone, name: "clone"},    // stops only when it makes a namespace
}
