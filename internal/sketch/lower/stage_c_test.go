package lower

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Exercise the public CLI, including its checker, generated module and
// runtime. A successful check alone missed the former reachable end panic.
func TestErgoRunFunctionTails(t *testing.T) {
	src := `type Failed { Code int }
func value() int { 42 }
func small() int8 { -128 }
func early(n int) int { if n < 0 { return 0 }; n }
func literal() int { let f = func() int { 7 }; f() }
func ignored() { 123 }
func id[T](v T) T { return v }
func ignoredFunction() { id[int] }
func fail() int fails Failed { Failed{Code: 3} }
func through() int fails Failed { fail() }
func main() needs Console {
	ignored()
	ignoredFunction()
	if value() == 42 && int(small()) == -128 && early(3) == 3 && literal() == 7 {
		Console.Println("tails")
	}
	let n = through() catch { case Failed: 9 }
	if n == 9 { Console.Println("failure") }
}
`
	path := filepath.Join(t.TempDir(), "main.ergo")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", "./cmd/ergo", "run", path)
	cmd.Dir = filepath.Clean("../../..")
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != "tails\nfailure\n" {
		t.Fatalf("ergo run: %v\n%s", err, out)
	}
}
