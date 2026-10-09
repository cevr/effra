package lintpacks

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	"effra.local/prototype/internal/compiler"
	"effra.local/prototype/internal/producer"
	sourcefile "effra.local/prototype/internal/source"
	"effra.local/prototype/lint"
)

// ReceiptKind and ReceiptVersion identify the cost receipt format.
const (
	ReceiptKind    = "effra.lint.cost-receipt"
	ReceiptVersion = 1
	// MaxReceiptRuns bounds one receipt.
	MaxReceiptRuns = 100
)

// Receipt is a raw lint cost receipt: where each analysis of one source
// under one lint configuration spent its wall time, phase by phase, with
// every run kept. It makes no claim: no aggregate, no baseline, no
// comparison. Frontend checking is measured apart from lint, and each
// pack's fact serialization and process apart from both.
type Receipt struct {
	Kind          string            `json:"kind"`
	Version       int               `json:"version"`
	Claim         string            `json:"claim"`
	Host          ReceiptHost       `json:"host"`
	Producer      producer.Identity `json:"producer"`
	Source        ReceiptSource     `json:"source"`
	Configuration string            `json:"configuration"`
	Rules         []lint.RuleInfo   `json:"rules"`
	Runs          []ReceiptRun      `json:"runs"`
}

// ReceiptHost is the machine a receipt was taken on.
type ReceiptHost struct {
	GOOS      string `json:"goos"`
	GOARCH    string `json:"goarch"`
	CPUs      int    `json:"cpus"`
	GoVersion string `json:"goVersion"`
}

// ReceiptSource is the measured source.
type ReceiptSource struct {
	URI    string `json:"uri"`
	Bytes  int    `json:"bytes"`
	Target string `json:"target"`
}

// ReceiptRun is one complete analysis, measured in nanoseconds of wall
// time. Each run reads and compiles the source afresh and starts every
// enabled pack as a new process; run 0 is the first in this ef process.
type ReceiptRun struct {
	Run      int    `json:"run"`
	Revision string `json:"revision"`
	Checked  bool   `json:"checked"`
	// Frontend reads, compiles and checks the source.
	FrontendNanos int64 `json:"frontendNanos"`
	// Facts extracts the fact families the enabled pack rules require,
	// once for every pack. Zero when no pack runs (unchecked source or no
	// selected pack).
	FactsNanos int64 `json:"factsNanos"`
	// Packs is the wall time of every pack running in parallel, as every
	// surface runs them.
	PacksNanos int64 `json:"packsNanos"`
	// Lint runs the built-in rules, applies suppressions and merges the
	// pack reports.
	LintNanos int64            `json:"lintNanos"`
	Complete  bool             `json:"complete"`
	PackRuns  []ReceiptPackRun `json:"packRuns"`
}

// ReceiptPackRun is one pack's share of a run; see lint.Trace.
type ReceiptPackRun struct {
	Pack           string                 `json:"pack"`
	Started        bool                   `json:"started"`
	Rules          []lint.RuleStatus      `json:"rules"`
	Failure        *lint.ExecutionFailure `json:"failure,omitempty"`
	PrepareNanos   int64                  `json:"prepareNanos"`
	SnapshotBytes  int                    `json:"snapshotBytes"`
	QualifyNanos   int64                  `json:"qualifyNanos"`
	EncodeNanos    int64                  `json:"encodeNanos"`
	RequestBytes   int                    `json:"requestBytes"`
	SpawnNanos     int64                  `json:"spawnNanos"`
	FirstByteNanos int64                  `json:"firstByteNanos"`
	ExitNanos      int64                  `json:"exitNanos"`
	ResponseBytes  int                    `json:"responseBytes"`
	AcceptNanos    int64                  `json:"acceptNanos"`
}

// Receipt analyses the source at path runs times through the same session,
// pack runner and merge as ef lint, recording the raw cost of each phase.
// A source that does not check is still measured: its runs show the
// frontend alone, since no pack runs over unchecked source.
func (s *Session) Receipt(ctx context.Context, path, target string, runs int) (Receipt, error) {
	if runs < 1 || runs > MaxReceiptRuns {
		return Receipt{}, fmt.Errorf("runs must be between 1 and %d", MaxReceiptRuns)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return Receipt{}, err
	}
	uri, err := compiler.FileURI(absolute)
	if err != nil {
		return Receipt{}, err
	}
	receipt := Receipt{
		Kind: ReceiptKind, Version: ReceiptVersion,
		Claim:         "raw wall-clock measurements of one host; no aggregate, baseline or performance claim",
		Host:          ReceiptHost{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, CPUs: runtime.NumCPU(), GoVersion: runtime.Version()},
		Producer:      producer.Current(),
		Source:        ReceiptSource{URI: uri, Target: target},
		Configuration: s.Configuration().Identity(),
		Rules:         s.Rules(),
		Runs:          []ReceiptRun{},
	}
	for run := range runs {
		began := time.Now()
		text, err := sourcefile.ReadRegularFile(absolute, 0)
		if err != nil {
			return Receipt{}, err
		}
		result := compiler.CompileAt(string(text), target, filepath.Dir(absolute))
		measured := ReceiptRun{Run: run, Revision: result.Revision, Checked: result.Checked, FrontendNanos: time.Since(began).Nanoseconds(), PackRuns: []ReceiptPackRun{}}
		receipt.Source.Bytes = len(text)
		var traces runTraces
		packs := s.run(ctx, result, compiler.SourceSnapshot{URI: uri, Origin: "disk", Text: string(text)}, &traces)
		measured.FactsNanos, measured.PacksNanos = traces.Facts.Nanoseconds(), traces.Wall.Nanoseconds()
		began = time.Now()
		merged := result.LintWith(false, packs)
		measured.LintNanos, measured.Complete = time.Since(began).Nanoseconds(), merged.Complete
		for i, pack := range packs.Reports {
			trace := traces.Packs[i]
			measured.PackRuns = append(measured.PackRuns, ReceiptPackRun{
				Pack: pack.Pack, Started: trace.Started, Rules: pack.Report.Rules, Failure: pack.Report.Failure,
				PrepareNanos: trace.Prepare.Nanoseconds(), SnapshotBytes: trace.SnapshotBytes,
				QualifyNanos: trace.Qualify.Nanoseconds(), EncodeNanos: trace.Encode.Nanoseconds(), RequestBytes: trace.RequestBytes,
				SpawnNanos: trace.Spawn.Nanoseconds(), FirstByteNanos: trace.FirstByte.Nanoseconds(), ExitNanos: trace.Exit.Nanoseconds(),
				ResponseBytes: trace.ResponseBytes, AcceptNanos: trace.Accept.Nanoseconds(),
			})
		}
		receipt.Runs = append(receipt.Runs, measured)
	}
	return receipt, nil
}
