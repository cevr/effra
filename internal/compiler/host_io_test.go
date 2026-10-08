package compiler

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"

	"effra.local/prototype/examples/hosttypes"
)

const hostIOImport = `import go host "effra.local/prototype/examples/hosttypes"
import go fs "io/fs"
import go io "io"
import go os "os"
import go iotest "testing/iotest"
import go strings "strings"
import go strconv "strconv"
import Data "effra/data"
`

func compileHostIO(t *testing.T, source string) *Result {
	t.Helper()
	return CompileAt(hostIOImport+source+hostTypesEntry, "go", "../..")
}

// The original file system reaches io/fs helpers, so fs.ReadDir takes the
// ReadDirFS fast path exactly when the native object has it and otherwise
// opens the directory and uses the file's own ReadDir. Directory files reach
// fs.ReadDirFile through a checked assertion, and an operating-system FS works
// the same way. A native Go control makes the same calls.
func TestHostFileSystemKeepsOptionalDirectoryDispatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("disk"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	r := compileHostIO(t, `effect fn names(listed: Data.Option<[]fs.DirEntry>) -> string uses { Foreign } {
    match listed {
        Data.Option.None => "nil",
        Data.Option.Some { value } => run host.EntryNames(value)
    }
}
effect fn fast() -> string uses { Foreign } {
    match run host.NewTree("b.txt,a.txt,sub/c.txt") {
        Data.Option.None => "no tree",
        Data.Option.Some { value: tree } => {
            let listed = run fs.ReadDir(tree, ".")
            run names(listed.value) + " " + run tree.Report()
        }
    }
}
effect fn flat() -> string uses { Foreign } {
    match run host.NewFlatTree("b.txt,a.txt,sub/c.txt") {
        Data.Option.None => "no tree",
        Data.Option.Some { value: tree } => {
            let listed = run fs.ReadDir(tree, ".")
            run names(listed.value) + " " + run tree.Report()
        }
    }
}
effect fn direct() -> string uses { Foreign } {
    match run host.NewFlatTree("b.txt,a.txt") {
        Data.Option.None => "no tree",
        Data.Option.Some { value: tree } => {
            let opened = run tree.Open(".")
            match opened.value {
                Data.Option.None => "nil file",
                Data.Option.Some { value: file } => {
                    let directory = file.as<fs.ReadDirFile>()
                    match directory.v0 {
                        Data.Option.None => "not a directory",
                        Data.Option.Some { value: listing } => run names((run listing.ReadDir(0)).value)
                    }
                }
            }
        }
    }
}
effect fn disk(root: string) -> string uses { Foreign } {
    match run os.DirFS(root) {
        Data.Option.None => "nil fs",
        Data.Option.Some { value: system } => {
            let listed = run fs.ReadDir(system, ".")
            let read = run fs.ReadFile(system, "a.txt")
            let text = match read.value {
                Data.Option.None => "nil",
                Data.Option.Some { value } => run host.BytesClass(value)
            }
            let info = run fs.Stat(system, "sub")
            let kind = match info.value {
                Data.Option.None => "nil",
                Data.Option.Some { value: stat } => run stat.Name() + " " + run strconv.FormatBool(run stat.IsDir())
            }
            let missing = run fs.ReadFile(system, "missing.txt")
            run names(listed.value) + " " + text + " " + kind + " " + run strconv.FormatBool(missing.hasError)
        }
    }
}
effect fn program() -> void uses { Console, Foreign } {
    run Console.log(run fast())
    run Console.log(run flat())
    run Console.log(run direct())
    run Console.log(run disk(`+strconv.Quote(dir)+`))
}`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	tree, flatTree := hosttypes.NewTree("b.txt,a.txt,sub/c.txt"), hosttypes.NewFlatTree("b.txt,a.txt,sub/c.txt")
	treeEntries, err := fs.ReadDir(tree, ".")
	if err != nil {
		t.Fatal(err)
	}
	flatEntries, err := fs.ReadDir(flatTree, ".")
	if err != nil {
		t.Fatal(err)
	}
	opened, err := hosttypes.NewFlatTree("b.txt,a.txt").Open(".")
	if err != nil {
		t.Fatal(err)
	}
	listing, err := opened.(fs.ReadDirFile).ReadDir(0)
	if err != nil {
		t.Fatal(err)
	}
	system := os.DirFS(dir)
	diskEntries, _ := fs.ReadDir(system, ".")
	diskText, _ := fs.ReadFile(system, "a.txt")
	info, _ := fs.Stat(system, "sub")
	_, missing := fs.ReadFile(system, "missing.txt")
	want := strings.Join([]string{
		hosttypes.EntryNames(treeEntries) + " " + tree.Report(),
		hosttypes.EntryNames(flatEntries) + " " + flatTree.Report(),
		hosttypes.EntryNames(listing),
		hosttypes.EntryNames(diskEntries) + " " + string(diskText) + " " + info.Name() + " " + strconv.FormatBool(info.IsDir()) + " " + strconv.FormatBool(missing != nil),
	}, "\n") + "\n"
	if !strings.Contains(want, "open=0 readDir=1") || !strings.Contains(want, "a.txt,b.txt,sub open=1\n") {
		t.Fatalf("native control did not take both directory paths: %q", want)
	}
	if output := runGeneratedGo(t, r); output != want {
		t.Fatalf("file system dispatch:\n%s\nnative:\n%s", output, want)
	}
}

// io.Reader may return data together with io.EOF. A read takes the buffer
// length and yields a fresh copy of the filled prefix, so the data and the
// original io.EOF arrive together and no byte is dropped. The same calls made
// directly in Go fix the expected sequence.
func TestHostReadSurfacesDataWithEndOfInput(t *testing.T) {
	r := compileHostIO(t, `effect fn end(read: Data.Option<error>) -> string uses { Foreign } {
    match read {
        Data.Option.None => "nil",
        Data.Option.Some { value: err } => if run host.IsEOF(err) { "EOF" } else { "other" }
    }
}
effect fn drain(reader: io.Reader) -> string uses { Foreign } {
    let first = run reader.Read(8)
    let second = run reader.Read(8)
    run host.BytesClass(first.value) + " " + run end(first.error) + "; " + run host.BytesClass(second.value) + " " + run end(second.error)
}
effect fn program() -> void uses { Console, Foreign } {
    match run strings.NewReader("abc") {
        Data.Option.None => void,
        Data.Option.Some { value: text } => match run iotest.DataErrReader(text) {
            Data.Option.None => void,
            Data.Option.Some { value: reader } => run Console.log(run drain(reader))
        }
    }
    match run strings.NewReader("abc") {
        Data.Option.None => void,
        Data.Option.Some { value: text } => {
            let first = run text.Read(2)
            let rest = run text.Read(2)
            let last = run text.Read(2)
            run Console.log(run host.BytesClass(first.value) + " " + run host.BytesClass(rest.value) + " " + run host.BytesClass(last.value) + " " + run end(last.error))
        }
    }
}`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	class := func(data []byte, err error) string {
		end := "nil"
		if err != nil {
			end = "other"
			if hosttypes.IsEOF(err) {
				end = "EOF"
			}
		}
		return hosttypes.BytesClass(data) + " " + end
	}
	read := func(reader io.Reader, size int) ([]byte, error) {
		buffer := make([]byte, size)
		n, err := reader.Read(buffer)
		return buffer[:n], err
	}
	reader := iotest.DataErrReader(strings.NewReader("abc"))
	first, firstErr := read(reader, 8)
	second, secondErr := read(reader, 8)
	text := strings.NewReader("abc")
	a, _ := read(text, 2)
	b, _ := read(text, 2)
	c, cErr := read(text, 2)
	want := class(first, firstErr) + "; " + class(second, secondErr) + "\n" + hosttypes.BytesClass(a) + " " + hosttypes.BytesClass(b) + " " + class(c, cErr) + "\n"
	if want != "abc EOF; empty EOF\nab c empty EOF\n" {
		t.Fatalf("native control did not return data with io.EOF: %q", want)
	}
	if output := runGeneratedGo(t, r); output != want {
		t.Fatalf("reads:\n%s\nnative:\n%s", output, want)
	}
}

func TestHostReaderSeekPositionalArguments(t *testing.T) {
	r := compileHostIO(t, `effect fn program() -> void uses { Console, Foreign } {
    match run strings.NewReader("abc") {
        Data.Option.None => void,
        Data.Option.Some { value: reader } => {
            let position = run reader.Seek(2, 0)
            run Console.log(run strconv.FormatInt(position.value, 10))
        }
    }
}`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	if output := runGeneratedGo(t, r); output != "2\n" {
		t.Fatalf("positional Seek(2, 0) = %q, want 2", output)
	}
}

// io.ReaderAt reads at an explicit offset without moving or depending on the
// reader's own position, and a read reaching the end returns its data with
// io.EOF. Interleaved Read and ReadAt calls match native Go.
func TestHostReadAtIsOffsetIndependent(t *testing.T) {
	r := compileHostIO(t, `effect fn end(read: Data.Option<error>) -> string uses { Foreign } {
    match read {
        Data.Option.None => "nil",
        Data.Option.Some { value: err } => if run host.IsEOF(err) { "EOF" } else { "other" }
    }
}
effect fn program() -> void uses { Console, Foreign } {
    match run strings.NewReader("abcdef") {
        Data.Option.None => void,
        Data.Option.Some { value: text } => {
            let a = run text.Read(2)
            let b = run text.ReadAt(3, 1)
            let c = run text.Read(2)
            let d = run text.ReadAt(4, 4)
            let e = run text.ReadAt(2, 9)
            run Console.log(run host.BytesClass(a.value) + " " + run host.BytesClass(b.value) + " " + run end(b.error) + " " + run host.BytesClass(c.value) + " " + run host.BytesClass(d.value) + " " + run end(d.error) + " " + run host.BytesClass(e.value) + " " + run end(e.error))
        }
    }
}`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	text := strings.NewReader("abcdef")
	class := func(data []byte, err error) string {
		end := "nil"
		if err != nil {
			end = "other"
			if hosttypes.IsEOF(err) {
				end = "EOF"
			}
		}
		return hosttypes.BytesClass(data) + " " + end
	}
	readAt := func(size int, offset int64) ([]byte, error) {
		buffer := make([]byte, size)
		n, err := text.ReadAt(buffer, offset)
		return buffer[:n], err
	}
	read := func(size int) []byte {
		buffer := make([]byte, size)
		n, _ := text.Read(buffer)
		return buffer[:n]
	}
	a := read(2)
	b, bErr := readAt(3, 1)
	c := read(2)
	d, dErr := readAt(4, 4)
	e, eErr := readAt(2, 9)
	want := hosttypes.BytesClass(a) + " " + class(b, bErr) + " " + hosttypes.BytesClass(c) + " " + class(d, dErr) + " " + class(e, eErr) + "\n"
	if want != "ab bcd nil cd ef EOF empty EOF\n" {
		t.Fatalf("native control: %q", want)
	}
	if output := runGeneratedGo(t, r); output != want {
		t.Fatalf("offset reads:\n%s\nnative:\n%s", output, want)
	}
}

// io.Writer must return a non-nil error when it accepts fewer bytes than it
// was given. A short write with a nil error is reported as io.ErrShortWrite
// with the accepted count, as Go's own io.Copy reports it; a well-behaved
// writer is unchanged.
func TestHostShortWriteReportsErrShortWrite(t *testing.T) {
	r := compileHostIO(t, `effect fn program() -> void uses { Console, Foreign } {
    match run host.RawText("hello") {
        Data.Option.None => void,
        Data.Option.Some { value: data } => {
            match run host.NewShortWriter(2) {
                Data.Option.None => void,
                Data.Option.Some { value: short } => {
                    let written = run short.Write(data)
                    let reported = match written.error {
                        Data.Option.None => "nil",
                        Data.Option.Some { value: err } => run strconv.FormatBool(run host.IsShortWrite(err))
                    }
                    run Console.log(run strconv.Itoa(written.value) + " " + run strconv.FormatBool(written.hasError) + " " + reported + " " + run short.Text())
                }
            }
            match run host.NewBuffer("") {
                Data.Option.None => void,
                Data.Option.Some { value: buffer } => {
                    let written = run buffer.Write(data)
                    run Console.log(run strconv.Itoa(written.value) + " " + run strconv.FormatBool(written.hasError) + " " + run buffer.String())
                }
            }
        }
    }
}`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	// The direct native call returns the violation; Go's own io.Copy reports it
	// as io.ErrShortWrite with the accepted count.
	direct := hosttypes.NewShortWriter(2)
	if n, err := direct.Write([]byte("hello")); n != 2 || err != nil {
		t.Fatalf("fixture no longer violates io.Writer: %d %v", n, err)
	}
	copied := hosttypes.NewShortWriter(2)
	n, err := io.Copy(copied, strings.NewReader("hello"))
	want := strconv.FormatInt(n, 10) + " " + strconv.FormatBool(err != nil) + " " + strconv.FormatBool(hosttypes.IsShortWrite(err)) + " " + copied.Text() + "\n5 false hello\n"
	if want != "2 true true he\n5 false hello\n" {
		t.Fatalf("native control: %q", want)
	}
	if output := runGeneratedGo(t, r); output != want {
		t.Fatalf("short write:\n%s\nnative:\n%s", output, want)
	}
}

// A count outside the buffer breaks the I/O contract with no meaningful
// partial value, so it is a defect rather than a typed failure.
func TestHostImpossibleIOCountsAreDefects(t *testing.T) {
	for _, tc := range []struct{ name, call, message string }{
		{"read", `let read = run broken.Read(4)`, "(*host.Overcount).Read returned 5 bytes for a 4-byte buffer"},
		{"write", `let written = run broken.Write(data)`, "(*host.Overcount).Write returned 6 bytes for a 5-byte buffer"},
		{"negative buffer", `let read = run broken.Read(run host.Negate(1))`, "(*host.Overcount).Read buffer length -1 is negative"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := compileHostIO(t, `effect fn program() -> void uses { Console, Foreign } {
    match run host.RawText("hello") {
        Data.Option.None => void,
        Data.Option.Some { value: data } => match run host.NewOvercount() {
            Data.Option.None => void,
            Data.Option.Some { value: broken } => {
                `+tc.call+`
                run Console.log("unreachable")
            }
        }
    }
}`)
			if !r.Checked {
				t.Fatal(r.Diagnostics)
			}
			code, application, err := emitGoApplication(r, GoGenerationBuild)
			if err != nil {
				t.Fatal(err)
			}
			output, err := execGoModule(t, application, r, map[string][]byte{"main.go": []byte(code)})
			if err == nil || strings.Contains(string(output), "unreachable") || !strings.Contains(string(output), tc.message) {
				t.Fatalf("expected defect %q: %v\n%s", tc.message, err, output)
			}
		})
	}
}

// I/O protocol methods are reported with their adapted shapes; a Read
// argument is a buffer length, not an Effra bytes value Go could overwrite.
func TestHostIOProtocolBindingsAndRefusals(t *testing.T) {
	r := compileHostIO(t, `effect fn program() -> void uses { Console, Foreign } {
    match run strings.NewReader("abc") {
        Data.Option.None => void,
        Data.Option.Some { value: text } => {
            let read = run text.ReadAt(2, 0)
            match run host.NewShortWriter(2) {
                Data.Option.None => void,
                Data.Option.Some { value: short } => {
                    let written = run short.Write(read.value)
                    void
                }
            }
        }
    }
}`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	bindings := map[string]Binding{}
	for _, binding := range r.Bindings {
		bindings[binding.Symbol] = binding
	}
	readAt, write := bindings["(*strings.Reader).ReadAt"], bindings["(*host.ShortWriter).Write"]
	if readAt.Protocol != "io.ReaderAt" || readAt.Return != "bytes" || len(readAt.HostParameters) != 3 || readAt.HostParameters[1] != (HostComponent{Native: "[]uint8", Type: "int", Adaptation: hostAdaptBuffer}) || readAt.HostResults[0] != (HostComponent{Native: "int", Type: "bytes", Adaptation: hostAdaptFilled}) {
		t.Fatalf("ReadAt binding: %+v", readAt)
	}
	if write.Protocol != "io.Writer" || write.Return != "int" || write.HostParameters[1].Adaptation != hostAdaptPresent || write.HostResults[0] != (HostComponent{Native: "int", Type: "int", Adaptation: hostAdaptWritten}) {
		t.Fatalf("Write binding: %+v", write)
	}
	for _, tc := range []struct{ body, message string }{
		{`match run host.RawText("x") {
        Data.Option.None => void,
        Data.Option.Some { value: data } => match run strings.NewReader("abc") {
            Data.Option.None => void,
            Data.Option.Some { value: text } => {
                let read = run text.Read(data)
                void
            }
        }
    }`, "Go argument must be int"},
		{`let n = 8
    match run strings.NewReader("abc") {
        Data.Option.None => void,
        Data.Option.Some { value: text } => {
            let read = run text.Read(n)
            void
        }
    }`, "Go argument must be int"},
	} {
		r := compileHostIO(t, "effect fn program() -> void uses { Console, Foreign } {\n    "+tc.body+"\n}")
		if r.Checked || !hasDiagnosticContaining(r, tc.message) {
			t.Fatalf("expected %q: %+v", tc.message, r.Diagnostics)
		}
	}
	js := CompileAt(hostIOImport+`effect fn main() -> void {
    void
}`, "js", "../..")
	if js.Checked || !hasCode(js, "EF110") || js.Timings.ImportMicros != 0 || len(js.Bindings) != 0 {
		t.Fatalf("JS target admitted Go I/O protocols: %+v", js.Diagnostics)
	}
}

// The application plan retains io for a checked write although source never
// imports it.
func TestApplicationPlanRetainsIOProtocolPackages(t *testing.T) {
	r := compileHostTypes(t, `effect fn program() -> void uses { Console, Foreign } {
    match run host.RawText("hello") {
        Data.Option.None => void,
        Data.Option.Some { value: data } => match run host.NewShortWriter(2) {
            Data.Option.None => void,
            Data.Option.Some { value: short } => {
                let written = run short.Write(data)
                run Console.log(run short.Text())
            }
        }
    }
}`)
	if !r.Checked {
		t.Fatal(r.Diagnostics)
	}
	plan, err := r.ApplicationPlan(GoGenerationBuild)
	if err != nil {
		t.Fatal(err)
	}
	write := "go:(*effra.local/prototype/examples/hosttypes.ShortWriter).Write"
	requirePlanned(t, plan, RequiresForeign, write)
	requireProvenance(t, plan, RequiresGoImport, "io", write, "io-protocol")
	if output := runGeneratedGo(t, r); output != "he\n" {
		t.Fatalf("short write without an io import: %q", output)
	}
}
