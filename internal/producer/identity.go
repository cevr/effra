// Package producer identifies the executing compiler host. Acquisition is lazy
// and separate from semantic compilation and private interface compatibility.
package producer

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"sync"
)

// Identity is passive, bounded metadata. Artifact identity qualifies reuse; the
// declaration is informative and never participates in that key.
type Identity struct {
	Strength    string      `json:"strength"`
	Digest      string      `json:"digest,omitempty"`
	Qualifier   string      `json:"qualifier"`
	ReuseScope  string      `json:"reuseScope"`
	Reason      string      `json:"reason,omitempty"`
	Declaration Declaration `json:"declaration,omitzero"`
}

type Declaration struct {
	GoVersion   string `json:"goVersion,omitempty"`
	Module      string `json:"module,omitempty"`
	Version     string `json:"version,omitempty"`
	VCSRevision string `json:"vcsRevision,omitempty"`
	VCSModified string `json:"vcsModified,omitempty"`
	GOOS        string `json:"goos,omitempty"`
	GOARCH      string `json:"goarch,omitempty"`
	CGOEnabled  string `json:"cgoEnabled,omitempty"`
}

var current = acquireOnce(func() (io.ReadCloser, error) { return openImage(runtime.GOOS) }, debug.ReadBuildInfo, processNonce)

func processNonce() (string, error) {
	var bytes [32]byte
	_, err := rand.Read(bytes[:])
	return hex.EncodeToString(bytes[:]), err
}

// Current acquires exactly once, including failure. It never follows an
// installation pathname, executes Git, or publishes arbitrary build settings.
func Current() Identity { return current() }

func openImage(goos string) (io.ReadCloser, error) {
	if goos != "linux" {
		return nil, errUnsupported
	}
	return os.Open("/proc/self/exe")
}

type unsupported struct{}

func (unsupported) Error() string { return "executing image identity unsupported" }

var errUnsupported = unsupported{}

func acquireOnce(open func() (io.ReadCloser, error), info func() (*debug.BuildInfo, bool), nonce func() (string, error)) func() Identity {
	return sync.OnceValue(func() Identity {
		identity := Identity{Strength: "unavailable", ReuseScope: "none"}
		fallback := func() Identity {
			if key, err := nonce(); err == nil && key != "" {
				identity.ReuseScope, identity.Qualifier = "process", "process:"+key
			}
			return identity
		}
		build, ok := info()
		identity.Declaration = declaration(build, ok)
		image, err := open()
		if err != nil {
			identity.Reason = "executing image cannot be opened"
			if err == errUnsupported {
				identity.Reason = "executing image identity requires Linux procfs"
			}
			return fallback()
		}
		defer image.Close()
		hash := sha256.New()
		// A large embedding host cannot make acquisition unbounded. This cap
		// limits streamed work, not the semantic projection's metadata budget.
		const maxImageBytes = 256 << 20
		count, err := io.Copy(hash, io.LimitReader(image, maxImageBytes+1))
		if err != nil || count > maxImageBytes {
			identity.Reason = "executing image cannot be read within its byte limit"
			return fallback()
		}
		identity.Strength, identity.ReuseScope = "executing-artifact", "artifact"
		identity.Digest = "sha256:" + hex.EncodeToString(hash.Sum(nil))
		identity.Qualifier = identity.Digest
		return identity
	})
}

func declaration(info *debug.BuildInfo, ok bool) Declaration {
	if !ok || info == nil {
		return Declaration{}
	}
	bounded := func(value string) string {
		if len(value) > 256 {
			return ""
		}
		return value
	}
	d := Declaration{GoVersion: bounded(info.GoVersion), Module: bounded(info.Main.Path), Version: bounded(info.Main.Version)}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			d.VCSRevision = bounded(setting.Value)
		case "vcs.modified":
			if setting.Value == "true" || setting.Value == "false" {
				d.VCSModified = setting.Value
			}
		case "GOOS":
			d.GOOS = bounded(setting.Value)
		case "GOARCH":
			d.GOARCH = bounded(setting.Value)
		case "CGO_ENABLED":
			if setting.Value == "0" || setting.Value == "1" {
				d.CGOEnabled = setting.Value
			}
		}
	}
	return d
}
