package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	rt "ergo.local/prototype/runtime/ergo"
)

// codecBenchLevels nests the deep cases' records: a LevelK holds two
// LevelK-1 fields inline, so a Level16 is one contiguous struct of 2^16
// transformed leaves (one MiB) in generated Go.
const codecBenchLevels = 16

// codecBenchGroups is the wide case's field groups: each group is a string,
// an i64, a bool, an effect-transformed Tick and a Shape union, so Wide has
// five fields per group, all at depth one.
const codecBenchGroups = 64

// codecBenchSource declares the matched-measurement cases. deepJson encodes
// a Level16 of pure-transformed leaves and tickDeepJson a TLevel16 of
// effect-transformed ticks, each under an 80-byte budget that is exceeded
// long before the last leaf, so every transformation runs after a deferred
// body-too-large failure. wideJson encodes a Wide value that fits: strings
// that need escaping and validation, i64 spellings, bools, effect-transformed
// ticks and union variants. tickCode fails with Refused on "refuse".
func codecBenchSource() string {
	var source strings.Builder
	source.WriteString(`import Json "ergo/json"

error Refused

record Leaf {
    code: string
}

fn leafCode(leaf: Leaf) -> string {
    leaf.code
}

fn leafOf(code: string) -> Leaf {
    Leaf { code: code }
}

derive leafJson = Json.codec<Leaf>(maxBodyBytes: 64, maxDepth: 1, decode: leafOf, encode: leafCode)

record Tick {
    code: string
}

effect fn tickCode(tick: Tick) -> string raises { Refused } {
    if tick.code == "refuse" {
        fail Refused {}
    } else {
        tick.code
    }
}

effect fn tickOf(code: string) -> Tick {
    Tick { code: code }
}

derive tickJson = Json.codec<Tick>(maxBodyBytes: 64, maxDepth: 1, decode: tickOf, encode: tickCode)

enum Shape {
    Circle { radius: i64 }
    Label { text: string }
    Empty
}
`)
	for _, prefix := range []string{"", "T"} {
		below := map[string]string{"": "Leaf", "T": "Tick"}[prefix]
		for level := 1; level <= codecBenchLevels; level++ {
			name := prefix + "Level" + strconv.Itoa(level)
			fmt.Fprintf(&source, "\nrecord %s {\n    a: %s\n    b: %s\n}\n", name, below, below)
			below = name
		}
	}
	source.WriteString("\nrecord Wide {\n")
	for group := range codecBenchGroups {
		fmt.Fprintf(&source, "    s%[1]d: string\n    n%[1]d: i64\n    b%[1]d: bool\n    t%[1]d: Tick\n    u%[1]d: Shape\n", group)
	}
	fmt.Fprintf(&source, `}

derive deepJson = Json.codec<Level%[1]d>(maxBodyBytes: 80, maxDepth: %[2]d) with leafJson
derive tickDeepJson = Json.codec<TLevel%[1]d>(maxBodyBytes: 80, maxDepth: %[2]d) with tickJson
derive wideJson = Json.codec<Wide>(maxBodyBytes: 1048576, maxDepth: 3) with tickJson

effect fn encodeDeep(value: Level%[1]d) -> string raises { JsonEncodeFailure } {
    run deepJson.encode(value)
}

effect fn main() -> void raises { JsonDecodeFailure, JsonEncodeFailure, Refused } {
    let deep = run deepJson.decode("{}")
    let deepText = run encodeDeep(deep)
    let ticks = run tickDeepJson.decode("{}")
    let tickText = run tickDeepJson.encode(ticks)
    let wide = run wideJson.decode("{}")
    let wideText = run wideJson.encode(wide)
    void
}
`, codecBenchLevels, codecBenchLevels+1)
	return source.String()
}

// codecBenchControl writes the hand-written idiomatic Go encoder the
// generated writers are measured against, for the same generated types and
// the same contract: the body budget checked before any byte beyond it is
// built, UTF-8 validation before a string's bytes, JSON.stringify escaping,
// a failure-path stack, typed structural errors (ctlError) and halting
// transformation errors (ctlHalt), with the first structural failure
// deferred while the remaining transformations still run. It calls the same
// generated witness functions, so only the encoder differs. One function
// per type, fields by pointer, as a person would write it.
func codecBenchControl() string {
	var out strings.Builder
	key := func(head, name string) string { return head + rt.CodecSpelling(name) + ":" }
	out.WriteString(`// ctlError is the control's typed structural failure.
type ctlError struct {
	Reason string
	Path   []string
}

func (e *ctlError) Error() string { return "encode " + e.Reason }

// ctlHalt is a transformation's failure; it ends the pass unchanged.
type ctlHalt struct{ cause er.Cause }

func (h *ctlHalt) Error() string { return "halted: " + h.cause.Error() }

// ctlEnc is one encode pass: the output, its budget, the path of the field
// being written and the first structural failure.
type ctlEnc struct {
	out   []byte
	limit int
	path  []string
	err   *ctlError
}

func (e *ctlEnc) enter(name string) { e.path = append(e.path, name) }
func (e *ctlEnc) leave()            { e.path = e.path[:len(e.path)-1] }

func (e *ctlEnc) raw(text string) {
	if e.err != nil {
		return
	}
	if len(e.out)+len(text) > e.limit {
		e.err = &ctlError{Reason: "body-too-large"}
		return
	}
	e.out = append(e.out, text...)
}

// ctlEscaped is the JSON.stringify spelling length of each byte.
var ctlEscaped = func() (table [256]uint8) {
	for c := range table {
		switch {
		case c == '"' || c == '\\' || c == '\b' || c == '\f' || c == '\n' || c == '\r' || c == '\t':
			table[c] = 2
		case c < 0x20:
			table[c] = 6
		default:
			table[c] = 1
		}
	}
	return
}()

// str sizes the escaped string first, so nothing past the budget is built.
func (e *ctlEnc) str(text string) {
	if e.err != nil {
		return
	}
	if !utf8.ValidString(text) {
		e.err = &ctlError{Reason: "invalid-unicode", Path: slices.Clone(e.path)}
		return
	}
	size := 2
	for i := 0; i < len(text); i++ {
		size += int(ctlEscaped[text[i]])
	}
	if len(e.out)+size > e.limit {
		e.err = &ctlError{Reason: "body-too-large"}
		return
	}
	const hex = "0123456789abcdef"
	e.out = append(e.out, '"')
	start := 0
	for i := 0; i < len(text); i++ {
		c := text[i]
		if ctlEscaped[c] == 1 {
			continue
		}
		e.out = append(e.out, text[start:i]...)
		start = i + 1
		switch c {
		case '"', '\\':
			e.out = append(e.out, '\\', c)
		case '\b':
			e.out = append(e.out, '\\', 'b')
		case '\f':
			e.out = append(e.out, '\\', 'f')
		case '\n':
			e.out = append(e.out, '\\', 'n')
		case '\r':
			e.out = append(e.out, '\\', 'r')
		case '\t':
			e.out = append(e.out, '\\', 't')
		default:
			e.out = append(e.out, '\\', 'u', '0', '0', hex[c>>4], hex[c&0xf])
		}
	}
	e.out = append(e.out, text[start:]...)
	e.out = append(e.out, '"')
}

func (e *ctlEnc) i64(value int64) {
	if e.err != nil {
		return
	}
	var scratch [22]byte
	spelled := append(strconv.AppendInt(append(scratch[:0], '"'), value, 10), '"')
	if len(e.out)+len(spelled) > e.limit {
		e.err = &ctlError{Reason: "body-too-large"}
		return
	}
	e.out = append(e.out, spelled...)
}

func (e *ctlEnc) boolean(value bool) {
	if value {
		e.raw("true")
	} else {
		e.raw("false")
	}
}

func (e *ctlEnc) finish(halted error) ([]byte, error) {
	if halted != nil {
		return nil, halted
	}
	if e.err != nil {
		return nil, e.err
	}
	return e.out, nil
}

func ctlLeaf(e *ctlEnc, v *efType_Leaf) { e.str(efFunction_leafCode(*v)) }

func ctlTick(e *ctlEnc, ctx efContext, v *efType_Tick) error {
	code := efFunction_tickCode(*v)(ctx)
	if code.IsFailure() {
		return &ctlHalt{cause: code.Cause()}
	}
	e.str(code.Value)
	return nil
}
`)
	field := func(name string) string { return goFieldName(name) }
	for level := 1; level <= codecBenchLevels; level++ {
		below, tickBelow := "ctlLevel"+strconv.Itoa(level-1), "ctlTLevel"+strconv.Itoa(level-1)
		if level == 1 {
			below, tickBelow = "ctlLeaf", "ctlTick"
		}
		fmt.Fprintf(&out, "func ctlLevel%[1]d(e *ctlEnc, v *efType_Level%[1]d) {\ne.raw(%[2]q)\ne.enter(\"a\")\n%[4]s(e, &v.%[5]s)\ne.leave()\ne.raw(%[3]q)\ne.enter(\"b\")\n%[4]s(e, &v.%[6]s)\ne.leave()\ne.raw(\"}\")\n}\n", level, key("{", "a"), key(",", "b"), below, field("a"), field("b"))
		fmt.Fprintf(&out, "func ctlTLevel%[1]d(e *ctlEnc, ctx efContext, v *efType_TLevel%[1]d) error {\ne.raw(%[2]q)\ne.enter(\"a\")\nif err := %[4]s(e, ctx, &v.%[5]s); err != nil {\nreturn err\n}\ne.leave()\ne.raw(%[3]q)\ne.enter(\"b\")\nif err := %[4]s(e, ctx, &v.%[6]s); err != nil {\nreturn err\n}\ne.leave()\ne.raw(\"}\")\nreturn nil\n}\n", level, key("{", "a"), key(",", "b"), tickBelow, field("a"), field("b"))
	}
	tag := func(variant string) string {
		return "{" + rt.CodecSpelling(rt.CodecTagKey) + ":" + rt.CodecSpelling(variant)
	}
	fmt.Fprintf(&out, `func ctlShape(e *ctlEnc, v *efType_Shape) {
	if e.err != nil {
		return
	}
	switch x := (*v).(type) {
	case %[1]s:
		e.raw(%[4]q)
		e.enter("radius")
		e.i64(x.%[7]s)
		e.leave()
		e.raw("}")
	case %[2]s:
		e.raw(%[5]q)
		e.enter("text")
		e.str(x.%[8]s)
		e.leave()
		e.raw("}")
	case %[3]s:
		e.raw(%[6]q)
	default:
		panic("Shape holds no declared variant")
	}
}
`, goVariantType("Shape", "Circle"), goVariantType("Shape", "Label"), goVariantType("Shape", "Empty"), tag("Circle")+","+rt.CodecSpelling("radius")+":", tag("Label")+","+rt.CodecSpelling("text")+":", tag("Empty")+"}", field("radius"), field("text"))
	out.WriteString("func ctlWide(e *ctlEnc, ctx efContext, v *efType_Wide) error {\n")
	head := "{"
	for group := range codecBenchGroups {
		for _, kind := range []string{"s", "n", "b", "t", "u"} {
			name := kind + strconv.Itoa(group)
			fmt.Fprintf(&out, "e.raw(%q)\ne.enter(%q)\n", key(head, name), name)
			head = ","
			switch kind {
			case "s":
				fmt.Fprintf(&out, "e.str(v.%s)\n", field(name))
			case "n":
				fmt.Fprintf(&out, "e.i64(v.%s)\n", field(name))
			case "b":
				fmt.Fprintf(&out, "e.boolean(v.%s)\n", field(name))
			case "t":
				fmt.Fprintf(&out, "if err := ctlTick(e, ctx, &v.%s); err != nil {\nreturn err\n}\n", field(name))
			case "u":
				fmt.Fprintf(&out, "ctlShape(e, &v.%s)\n", field(name))
			}
			out.WriteString("e.leave()\n")
		}
	}
	out.WriteString("e.raw(\"}\")\nreturn nil\n}\n")
	fmt.Fprintf(&out, `func ctlEncodeDeep(v *efType_Level%[1]d) ([]byte, error) {
	e := ctlEnc{limit: 80, path: make([]string, 0, %[2]d)}
	ctlLevel%[1]d(&e, v)
	return e.finish(nil)
}

func ctlEncodeTickDeep(v *efType_TLevel%[1]d, ctx efContext) ([]byte, error) {
	e := ctlEnc{limit: 80, path: make([]string, 0, %[2]d)}
	return e.finish(ctlTLevel%[1]d(&e, ctx, v))
}

func ctlEncodeWide(v *efType_Wide, ctx efContext) ([]byte, error) {
	e := ctlEnc{limit: 1048576, path: make([]string, 0, 2)}
	return e.finish(ctlWide(&e, ctx, v))
}
`, codecBenchLevels, codecBenchLevels+1)
	return out.String()
}

// codecBenchValues writes the Go builders of the measured values, built as
// generated code nests records: inline, by value.
func codecBenchValues() string {
	var out strings.Builder
	field := func(name string) string { return goFieldName(name) }
	fmt.Fprintf(&out, "func level0() efType_Leaf { return efType_Leaf{%s: \"x\"} }\n", field("code"))
	fmt.Fprintf(&out, "func tlevel0() efType_Tick { return efType_Tick{%s: \"x\"} }\n", field("code"))
	for level := 1; level <= codecBenchLevels; level++ {
		fmt.Fprintf(&out, "func level%[1]d() efType_Level%[1]d { below := level%[2]d(); return efType_Level%[1]d{%[3]s: below, %[4]s: below} }\n", level, level-1, field("a"), field("b"))
		fmt.Fprintf(&out, "func tlevel%[1]d() efType_TLevel%[1]d { below := tlevel%[2]d(); return efType_TLevel%[1]d{%[3]s: below, %[4]s: below} }\n", level, level-1, field("a"), field("b"))
	}
	out.WriteString("var wideTexts = []string{\"plain\", \"quote\\\"back\\\\slash\", \"line\\nfeed\\ttab\\r\", \"ctl\\x01\\x1f\\b\\f\", \"é☃\U0001F600\", \"\\u2028sep\\u2029\"}\n")
	out.WriteString("func wide() efType_Wide {\nvar v efType_Wide\n")
	for group := range codecBenchGroups {
		shape := []string{
			fmt.Sprintf("%s{%s: %d}", goVariantType("Shape", "Circle"), field("radius"), group*7919-250000),
			fmt.Sprintf("%s{%s: wideTexts[%d]}", goVariantType("Shape", "Label"), field("text"), group%6),
			goVariantType("Shape", "Empty") + "{}",
		}[group%3]
		fmt.Fprintf(&out, "v.%s = wideTexts[%d]\nv.%s = %d\nv.%s = %t\nv.%s = efType_Tick{%s: \"tick%d\"}\nv.%s = %s\n",
			field("s"+strconv.Itoa(group)), group%6, field("n"+strconv.Itoa(group)), int64(group)*1234567891-40000000000, field("b"+strconv.Itoa(group)), group%2 == 0,
			field("t"+strconv.Itoa(group)), field("code"), group, field("u"+strconv.Itoa(group)), shape)
	}
	out.WriteString("return v\n}\n")
	return out.String()
}

// codecBenchProbe is the harness: TestEquivalence holds the control to the
// generated writer's observable behavior, and BenchmarkEncode measures both
// on each case. efProbeStep is prepended to each case's leaf witness
// function, which both encoders call, so the live heap is read at the last
// transformation of a pass and every step count is observed.
const codecBenchProbe = `package main

import (
	"runtime"
	"slices"
	"strconv"
	"testing"
	"unicode/utf8"

	er "ergo.generated/runtime"
)

var _ = utf8.ValidString

// probe counts witness calls; at the at'th call of a measured pass it
// collects and records the live heap above base.
var probe struct {
	calls, at int
	base      uint64
	retained  int64
}

func efProbeStep() {
	probe.calls++
	if probe.calls == probe.at {
		probeSnapshot()
	}
}

//go:noinline
func probeSnapshot() {
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	probe.retained = int64(stats.HeapAlloc) - int64(probe.base)
}

// measureRetained runs one pass and returns the heap it holds live at its
// last transformation, after a collection, above the heap before it.
func measureRetained(steps int, pass func()) int64 {
	probe.calls, probe.at = 0, steps
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	probe.base = stats.HeapAlloc
	pass()
	if probe.calls != steps {
		panic("pass ran " + strconv.Itoa(probe.calls) + " steps, want " + strconv.Itoa(steps))
	}
	probe.at = 0
	return probe.retained
}

@CONTROL@
@VALUES@

var (
	deepValue     = level@LEVELS@()
	tickDeepValue = tlevel@LEVELS@()
	wideValue     = wide()
	sinkBytes     []byte
	sinkErr       error
)

// encoders pairs each case's generated entry with its control.
type encoders struct {
	steps               int
	generated, control  func(ctx efContext) ([]byte, error)
}

var cases = []struct {
	name string
	encoders
}{
	{"deep-pure", encoders{1 << @LEVELS@, func(efContext) ([]byte, error) { return @DEEP@(&deepValue) }, func(efContext) ([]byte, error) { return ctlEncodeDeep(&deepValue) }}},
	{"deep-effect", encoders{1 << @LEVELS@, func(ctx efContext) ([]byte, error) { return @TICKDEEP@(&tickDeepValue, ctx) }, func(ctx efContext) ([]byte, error) { return ctlEncodeTickDeep(&tickDeepValue, ctx) }}},
	{"wide", encoders{@GROUPS@, func(ctx efContext) ([]byte, error) { return @WIDE@(&wideValue, ctx) }, func(ctx efContext) ([]byte, error) { return ctlEncodeWide(&wideValue, ctx) }}},
}

func inFiber(run func(ctx efContext)) {
	er.Run(func(fc *er.FiberContext) er.Exit[struct{}] {
		run(efContext{Runtime: fc})
		return er.Succeed(struct{}{})
	})
}

// outcome is an encoder's observable result: output, structural failure
// (reason and path) or halting failure tag, and the steps it ran.
type outcome struct {
	output, reason, halted string
	path                   []string
	steps                  int
}

func observe(t *testing.T, ctx efContext, encode func(efContext) ([]byte, error)) outcome {
	probe.calls, probe.at = 0, 0
	output, err := encode(ctx)
	seen := outcome{output: string(output), steps: probe.calls}
	switch failure := err.(type) {
	case nil:
	case *er.CodecError:
		seen.reason, seen.path = string(failure.Reason), failure.Path
	case *ctlError:
		seen.reason, seen.path = failure.Reason, failure.Path
	case *efCodecHalt:
		seen.halted = failure.cause[0].Failure.Tag
	case *ctlHalt:
		seen.halted = failure.cause[0].Failure.Tag
	default:
		t.Fatalf("unexpected error %T %v", err, err)
	}
	if len(seen.path) == 0 {
		seen.path = nil
	}
	return seen
}

func TestEquivalence(t *testing.T) {
	inFiber(func(ctx efContext) {
		check := func(name string, pair encoders, want outcome) {
			generated, control := observe(t, ctx, pair.generated), observe(t, ctx, pair.control)
			if generated.output != control.output || generated.reason != control.reason || generated.halted != control.halted || !slices.Equal(generated.path, control.path) || generated.steps != control.steps {
				t.Fatalf("%s: generated %+v, control %+v", name, generated, control)
			}
			want.output = generated.output
			if want.reason != generated.reason || want.halted != generated.halted || !slices.Equal(want.path, generated.path) || want.steps != generated.steps {
				t.Fatalf("%s: observed %+v, want %+v", name, generated, want)
			}
			t.Logf("%s: %d steps, %d output bytes, reason %q, halted %q", name, generated.steps, len(generated.output), generated.reason, generated.halted)
		}
		check("deep-pure", cases[0].encoders, outcome{reason: "body-too-large", steps: 1 << @LEVELS@})
		check("deep-effect", cases[1].encoders, outcome{reason: "body-too-large", steps: 1 << @LEVELS@})
		check("wide", cases[2].encoders, outcome{steps: @GROUPS@})
		if output, err := cases[2].generated(ctx); err != nil || !utf8.Valid(output) || len(output) < 1000 {
			t.Fatalf("wide output %q %v", output, err)
		}
		// Ill-formed text fails at its path; later transformations still run.
		saved := wideValue
		wideValue.@S3@ = "bad\xff"
		check("wide-invalid-unicode", cases[2].encoders, outcome{reason: "invalid-unicode", path: []string{"s3"}, steps: @GROUPS@})
		// A halting transformation ends the pass after a deferred failure.
		wideValue.@T5@.@CODE@ = "refuse"
		check("wide-halt", cases[2].encoders, outcome{halted: "Refused", steps: 6})
		wideValue = saved
		// The production direction reports the deferred failure as its own.
		deep := efFunction_encodeDeep(deepValue)(ctx)
		cause := deep.Cause()
		if !deep.IsFailure() || len(cause) != 1 || cause[0].Failure == nil || cause[0].Failure.Tag != "JsonEncodeFailure" {
			t.Fatalf("deep direction: %+v", deep)
		}
		if payload, ok := cause[0].Failure.Payload.(*er.CodecError); !ok || payload.Reason != er.CodecBodyTooLarge {
			t.Fatalf("deep direction payload: %+v", cause[0].Failure.Payload)
		}
	})
}

// BenchmarkEncode measures each encoder as built for production; the
// clean build's witness functions carry no probe.
func BenchmarkEncode(b *testing.B) {
	for _, c := range cases {
		for _, side := range []struct {
			name   string
			encode func(efContext) ([]byte, error)
		}{{"generated", c.generated}, {"control", c.control}} {
			b.Run(c.name+"/"+side.name, func(b *testing.B) {
				inFiber(func(ctx efContext) {
					b.ReportAllocs()
					for b.Loop() {
						sinkBytes, sinkErr = side.encode(ctx)
					}
				})
			})
		}
	}
}

// BenchmarkRetained reports, in the probed build, the heap one pass holds
// live at its last transformation.
func BenchmarkRetained(b *testing.B) {
	for _, c := range cases {
		for _, side := range []struct {
			name   string
			encode func(efContext) ([]byte, error)
		}{{"generated", c.generated}, {"control", c.control}} {
			b.Run(c.name+"/"+side.name, func(b *testing.B) {
				inFiber(func(ctx efContext) {
					var retained int64
					for b.Loop() {
						retained = measureRetained(c.steps, func() { sinkBytes, sinkErr = side.encode(ctx) })
					}
					b.ReportMetric(float64(retained), "retained-B/op")
				})
			})
		}
	}
}
`

// codecBenchRow is one measured metric of one case and encoder: its
// samples, one per -count run.
type codecBenchRow struct {
	samples []float64
}

func (r codecBenchRow) median() float64 {
	sorted := slices.Clone(r.samples)
	sort.Float64s(sorted)
	if n := len(sorted); n%2 == 1 {
		return sorted[n/2]
	} else {
		return (sorted[n/2-1] + sorted[n/2]) / 2
	}
}

func (r codecBenchRow) spread() string {
	return fmt.Sprintf("%.0f [%.0f..%.0f]", r.median(), slices.Min(r.samples), slices.Max(r.samples))
}

// codecBenchAllocSlack is the fixed number of allocations per pass the
// generated writer may make beyond the control, independent of the value:
// room for the runtime CodecWriter and its path stack should escape
// analysis ever place them differently from the control's encoder. At the
// time of writing both make the same number.
const codecBenchAllocSlack = 2

// The production generated Go encode entry, efCodecEncode_<plan>, with the
// witnesses' own generated step functions and the direction's context, is
// measured against a hand-written idiomatic Go encoder doing the same
// contract on the same generated types (codecBenchControl), on 2^16
// pure-transformed leaves held inline, on 2^16 effect-transformed leaves
// and on one wide record. TestEquivalence first holds the two to the same
// output bytes, structural failure reason and path, halting failure and
// step count, and the production direction to a JsonEncodeFailure. The
// benchmark then runs -count times; for each metric the median of the
// generated entry may exceed the control's by at most 1 KiB of bytes
// retained at the last transformation and bytes allocated, and by
// codecBenchAllocSlack allocations per pass, the same constant on the deep
// and wide cases. The deep generated entry also stays within 8 KiB
// retained and allocated over its one MiB value. ns/op is reported, with
// the ratio, but not bounded. ERGO_CODEC_BENCH_COUNT and
// ERGO_CODEC_BENCH_TIME override -count (default 10) and -benchtime
// (default 50ms); ERGO_CODEC_BENCH_OUT receives the table.
func TestCodecGeneratedGoEncodeMatchesHandWrittenControl(t *testing.T) {
	t.Parallel()
	r := CompileFor(codecBenchSource(), "go")
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	code, application, err := emitGoApplication(r, GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(code, "reflect") || strings.Contains(code, "efCodecSteps_") {
		t.Fatal("generated encode reflects or dispatches through a steps struct")
	}
	plans := map[string]*CodecPlan{}
	for _, codec := range r.Program.Codecs {
		plans[codec.Name] = codec.plan
	}
	probe := strings.NewReplacer(
		"@CONTROL@", codecBenchControl(), "@VALUES@", codecBenchValues(),
		"@LEVELS@", strconv.Itoa(codecBenchLevels), "@GROUPS@", strconv.Itoa(codecBenchGroups),
		"@DEEP@", goCodecEncodeName(plans["deepJson"]), "@TICKDEEP@", goCodecEncodeName(plans["tickDeepJson"]), "@WIDE@", goCodecEncodeName(plans["wideJson"]),
		"@S3@", goFieldName("s3"), "@T5@", goFieldName("t5"), "@CODE@", goFieldName("code"),
	).Replace(codecBenchProbe)
	// The clean build is the generated program as emitted; it measures time
	// and allocation. The probed build prepends efProbeStep to the two leaf
	// witness functions, which keeps the Go compiler from inlining
	// tickCode's recipe and so allocates one closure per effect step on both
	// sides; it checks equivalence and step counts and reads the retained
	// heap.
	probed := code
	for _, function := range []string{"efFunction_leafCode", "efFunction_tickCode"} {
		probed = codecGoInstrument(t, probed, function, "efProbeStep()")
	}
	build := func(main string) string {
		dir := t.TempDir()
		if err := application.WriteRuntime(dir); err != nil {
			t.Fatal(err)
		}
		for name, data := range map[string][]byte{"go.mod": r.ModuleFile(), "main.go": []byte(main), "main_test.go": []byte(probe)} {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	count, benchtime := os.Getenv("ERGO_CODEC_BENCH_COUNT"), os.Getenv("ERGO_CODEC_BENCH_TIME")
	if count == "" {
		count = "10"
	}
	if benchtime == "" {
		benchtime = "50ms"
	}
	retainedOutput, err := runGoCommand(build(probed), "test", "-v", "-run", "^TestEquivalence$", "-bench", "^BenchmarkRetained$", "-count", count, "-benchtime", "1x", ".")
	if err != nil {
		t.Fatalf("probed build: %v\n%s", err, retainedOutput)
	}
	output, err := runGoCommand(build(code), "test", "-run", "^$", "-bench", "^BenchmarkEncode$", "-benchmem", "-count", count, "-benchtime", benchtime, ".")
	if err != nil {
		t.Fatalf("clean build: %v\n%s", err, output)
	}
	rows := map[string]*codecBenchRow{}
	collect := func(output []byte, benchmark string, units ...string) {
		line := regexp.MustCompile(`(?m)^` + benchmark + `/([^/\s]+)/(generated|control)(?:-\d+)?\s+\d+\s+(.*)$`)
		for _, match := range line.FindAllStringSubmatch(string(output), -1) {
			fields := strings.Fields(match[3])
			for i := 0; i+1 < len(fields); i += 2 {
				if !slices.Contains(units, fields[i+1]) {
					continue
				}
				value, err := strconv.ParseFloat(fields[i], 64)
				if err != nil {
					t.Fatalf("benchmark line %q: %v", match[0], err)
				}
				id := match[1] + "/" + match[2] + "/" + fields[i+1]
				if rows[id] == nil {
					rows[id] = &codecBenchRow{}
				}
				rows[id].samples = append(rows[id].samples, value)
			}
		}
	}
	collect(retainedOutput, "BenchmarkRetained", "retained-B/op")
	collect(output, "BenchmarkEncode", "ns/op", "B/op", "allocs/op")
	output = append(retainedOutput, output...)
	var table strings.Builder
	fmt.Fprintf(&table, "count=%s benchtime=%s; median [min..max] over runs\n%-12s %-10s %-28s %-24s %-22s %-20s\n", count, benchtime, "case", "encoder", "ns/op", "retained-B/op", "B/op", "allocs/op")
	metric := func(name, encoder, unit string) codecBenchRow {
		row := rows[name+"/"+encoder+"/"+unit]
		if row == nil || strconv.Itoa(len(row.samples)) != count {
			t.Fatalf("%s/%s reported %v samples of %s, want %s\n%s", name, encoder, row, unit, count, output)
		}
		return *row
	}
	for _, name := range []string{"deep-pure", "deep-effect", "wide"} {
		for _, encoder := range []string{"generated", "control"} {
			fmt.Fprintf(&table, "%-12s %-10s %-28s %-24s %-22s %-20s\n", name, encoder, metric(name, encoder, "ns/op").spread(), metric(name, encoder, "retained-B/op").spread(), metric(name, encoder, "B/op").spread(), metric(name, encoder, "allocs/op").spread())
		}
		fmt.Fprintf(&table, "%-12s ns/op ratio generated/control %.3f\n", name, metric(name, "generated", "ns/op").median()/metric(name, "control", "ns/op").median())
	}
	t.Log("\n" + table.String())
	if path := os.Getenv("ERGO_CODEC_BENCH_OUT"); path != "" {
		if err := os.WriteFile(path, []byte(table.String()+"\n"+string(output)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"deep-pure", "deep-effect", "wide"} {
		for unit, slack := range map[string]float64{"retained-B/op": 1 << 10, "B/op": 1 << 10, "allocs/op": codecBenchAllocSlack} {
			if generated, control := metric(name, "generated", unit).median(), metric(name, "control", unit).median(); generated > control+slack {
				t.Errorf("%s: generated median %s %.0f exceeds the control's %.0f by more than %.0f", name, unit, generated, control, slack)
			}
		}
	}
	for _, unit := range []string{"retained-B/op", "B/op"} {
		if deep := metric("deep-pure", "generated", unit).median(); deep > 8<<10 {
			t.Errorf("encoding an 80-byte budget over %d inline leaves: median %s %.0f", 1<<codecBenchLevels, unit, deep)
		}
	}
}
