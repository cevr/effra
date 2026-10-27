package rttrace

import (
	"fmt"
	"io"
	"os"
	"reflect"

	"effra.local/prototype/runtime/rt"
)

// Schema names the JSON Lines format WriteJSONL writes.
const Schema = "effra-trace-2"

// Header is the first line of a JSON Lines trace. The revision is written
// once here; events carry plain definition IDs, which the revision qualifies.
type Header struct {
	Schema   string `json:"schema"`
	Revision string `json:"revision"`
	Producer string `json:"producer"`
	Capacity int    `json:"capacity"`
	Events   int    `json:"events"`
	Dropped  uint64 `json:"dropped"`
	InFlight int    `json:"inFlight,omitempty"`
}

// Line is one event line of a JSON Lines trace. ID and Link are the
// runtime's occurrence IDs (rt.Event): id orders events by when the runtime
// raised them, and link names the occurrence the event refers to.
type Line struct {
	Seq    uint64   `json:"seq"`
	Time   int64    `json:"time"` // nanoseconds since the recorder started
	ID     uint64   `json:"id"`
	Link   uint64   `json:"link,omitempty"`
	Kind   string   `json:"kind"`
	Task   uint64   `json:"task"`
	Parent uint64   `json:"parent"`
	Def    string   `json:"def"`
	Site   string   `json:"site,omitempty"`
	Cause  []Reason `json:"cause,omitempty"`
}

// Reason is one reason of a body-exit cause, the outcome of a call-exit, or
// the defect of a panic event. It names types, never values: a typed failure
// by its type name, a defect by the type of its panic value. A call-exit with
// no reason returned ok.
type Reason struct {
	Reason    string  `json:"reason"` // failure, defect or interrupted
	Name      string  `json:"name,omitempty"`
	InRelease bool    `json:"inRelease,omitempty"`
	Chain     []Frame `json:"chain,omitempty"`
}

// Frame is one task of a defect's chain (G2).
type Frame struct {
	Task uint64 `json:"task"`
	Def  string `json:"def"`
}

// Lines turns records into event lines. sites maps a definition ID to its
// source site; an event with no site of its own takes its definition's. A
// task's parent is the one its start event recorded, since a finished task
// has dropped its live parent link.
func Lines(recs []Record, sites map[string]string) []Line {
	parent := map[uint64]uint64{}
	for _, r := range recs {
		if r.Kind == rt.TraceStart {
			parent[r.Task] = r.Parent
		}
	}
	out := make([]Line, len(recs))
	for i, r := range recs {
		l := Line{Seq: r.Seq, Time: int64(r.Time), ID: r.ID, Link: r.Link, Kind: r.Kind.String(), Task: r.Task, Parent: r.Parent, Def: r.Def, Site: r.Site}
		if p, ok := parent[r.Task]; ok {
			l.Parent = p
		}
		if l.Site == "" {
			l.Site = sites[r.Def]
		}
		switch r.Kind {
		case rt.TraceBodyExit:
			l.Cause = Reasons(r.Cause)
		case rt.TraceCallExit:
			if r.Defect != nil {
				l.Cause = []Reason{{Reason: "defect", Name: typeName(r.Defect)}}
			} else {
				l.Cause = Reasons(r.Cause)
			}
		case rt.TracePanic:
			l.Cause = []Reason{{Reason: "defect", Name: typeName(r.Defect), InRelease: r.InRelease}}
		}
		out[i] = l
	}
	return out
}

// Reasons renders an exit by reason and type name: nil for success.
func Reasons(err error) []Reason {
	if err == nil {
		return nil
	}
	if err == rt.Interrupted {
		return []Reason{{Reason: "interrupted"}}
	}
	c, ok := err.(*rt.Cause)
	if !ok || c == nil {
		return []Reason{{Reason: "failure", Name: failureName(err)}}
	}
	out := make([]Reason, 0, len(c.Reasons))
	for _, r := range c.Reasons {
		switch r.Kind {
		case rt.KindFailure:
			out = append(out, Reason{Reason: "failure", Name: failureName(r.Err)})
		case rt.KindInterruption:
			out = append(out, Reason{Reason: "interrupted"})
		case rt.KindDefect:
			d := Reason{Reason: "defect", Name: typeName(r.Value), InRelease: r.InRelease}
			for _, f := range r.Chain {
				d.Chain = append(d.Chain, Frame{Task: f.Task, Def: f.Span})
			}
			out = append(out, d)
		}
	}
	return out
}

// failureName is the source name of a typed failure: timeout.Expired for the
// runtime's own, the Go type name otherwise (emitted code keeps source type
// names).
func failureName(err error) string {
	if _, ok := err.(rt.Expired); ok {
		return "timeout.Expired"
	}
	return typeName(err)
}

func typeName(v any) string {
	t := reflect.TypeOf(v)
	if t == nil {
		return "nil"
	}
	for t.Kind() == reflect.Pointer && t.Name() == "" {
		t = t.Elem()
	}
	if t.Name() != "" {
		return t.Name()
	}
	return t.String()
}

// WriteJSONL writes the header and one line per record to w.
func WriteJSONL(w io.Writer, h Header, recs []Record, sites map[string]string) error {
	h.Schema = Schema
	h.Events = len(recs)
	buf := h.appendJSON(nil)
	for _, l := range Lines(recs, sites) {
		buf = l.appendJSON(buf)
	}
	_, err := w.Write(buf)
	return err
}

// WriteFile writes r's kept events to path as JSON Lines. Call it after the
// traced tree has finished.
func WriteFile(path string, r *Recorder, revision, producer string, sites map[string]string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	recs, inFlight := r.Records()
	h := Header{Revision: revision, Producer: producer, Capacity: r.Cap(), Dropped: r.Dropped(), InFlight: inFlight}
	if err := WriteJSONL(f, h, recs, sites); err != nil {
		return fmt.Errorf("rttrace: write %s: %w", path, err)
	}
	return nil
}
