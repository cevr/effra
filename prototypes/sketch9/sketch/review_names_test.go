package sketch

import (
	"fmt"
	gotoken "go/token"
	"go/types"
	"strings"
	"testing"
)

func TestReviewNamePositions(t *testing.T) {
	positions := []struct{ name, body string }{
		{"type", `type NAME struct{}; func main(){}`},
		{"function", `func NAME(){}; func main(){}`},
		{"field", `type Box struct{ NAME string }; func main(){}`},
		{"method", `type Box struct{}; func (Box) NAME(){}; func main(){}`},
		{"capability-method", `type Cap interface{ NAME() }; func main(){}`},
		{"wire", `wire NAME{}; func main(){}`},
		{"parameter", `func f(NAME string){}; func main(){}`},
		{"receiver", `type Box struct{}; func (NAME Box) F(){}; func main(){}`},
		{"local", `func main() uses Console{ NAME := "ok"; Console.Println(NAME) }`},
		{"switch", `type A struct{ Value string }; type U interface{ A }; func makeU() U { return A{} }; func main() uses Console { switch NAME := makeU().(type) { case A: Console.Println(NAME.Value) } }`},
	}
	names := append([]string{}, types.Universe.Names()...)
	for kind := gotoken.BREAK; kind <= gotoken.VAR; kind++ {
		names = append(names, kind.String())
	}
	names = append(names, "rt", "fmt", "time", "init", "Error", "v__1", "e__2", "envc__3", "env__", "t__", "Env__", "e__", "plain__", "is__U", "wire__W", "main__")
	for _, position := range positions {
		for _, name := range names {
			t.Run(position.name+"/"+name, func(t *testing.T) {
				src := "package demo\n" + strings.ReplaceAll(position.body, "NAME", name)
				shouldReject := position.name != "field" || gotoken.Lookup(name).IsKeyword() || name == "Error" || strings.Contains(name, "__")
				file, err := Parse("name.sketch", src)
				rejected := err != nil
				var ds []Diagnostic
				if err == nil {
					_, ds = Check(file)
					rejected = len(ds) > 0
				}
				if rejected != shouldReject {
					t.Errorf("reject=%v want=%v parse=%v diagnostics=%v source=%s", rejected, shouldReject, err, ds, src)
				}
			})
		}
	}
}

func TestReviewSafeNameScopes(t *testing.T) {
	positions := []string{
		`type NAME struct{}; func main() uses Console { Console.Println("ok") }`,
		`func NAME() string { return "ok" }; func main() uses Console { Console.Println(NAME()) }`,
		`type Box struct{ NAME string }; func main() uses Console { Console.Println(Box{NAME: "ok"}.NAME) }`,
		`type Cap interface{ NAME() string }; type Box struct{}; func (Box) NAME() string{return "ok"}; func main() uses Console { with(wire{Cap=Box{}}){Console.Println(Cap.NAME())} }`,
		`wire NAME{}; func main() uses Console { with(NAME){Console.Println("ok")} }`,
		`func f(NAME string) string{return NAME}; func main() uses Console{Console.Println(f("ok"))}`,
		`type Box struct{Value string}; type Cap interface{Get() string}; func(NAME Box) Get() string{return NAME.Value}; func main() uses Console{with(wire{Cap=Box{Value:"ok"}}){Console.Println(Cap.Get())}}`,
		`func main() uses Console{NAME:="ok";Console.Println(NAME)}`,
		`type A struct{Value string};type U interface{A};func makeU() U{return A{Value:"ok"}};func main() uses Console{switch NAME:=makeU().(type){case A:Console.Println(NAME.Value)}}`,
	}
	for _, name := range []string{"envc", "e", "plain", "Item", "v", "Env"} {
		for i, body := range positions {
			t.Run(fmt.Sprintf("%s/%d", name, i), func(t *testing.T) {
				if got := buildRun(t, "package demo\n"+strings.ReplaceAll(body, "NAME", name)); got != "ok\n" {
					t.Fatalf("got %q", got)
				}
			})
		}
	}
}
