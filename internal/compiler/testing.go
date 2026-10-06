package compiler

import (
	"fmt"
	"strings"
)

// Tests are ordinary checked effects. Only assertions are supplied implicitly;
// fixture services are explicit so host access cannot appear through a preset.
func (r *Result) Tests() ([]*Symbol, error) {
	if !r.Checked {
		return nil, fmt.Errorf("tests require checked source")
	}
	tests := []*Symbol{}
	for i := range r.Symbols {
		s := &r.Symbols[i]
		if !strings.HasPrefix(s.Name, "test_") {
			continue
		}
		if !s.Contract.Effect || s.Contract.Success != "()" || len(s.Params) != 0 {
			return nil, fmt.Errorf("%s must be an effect function with no parameters returning ()", s.Name)
		}
		for _, req := range s.Contract.Services {
			if req != "Assert" {
				return nil, fmt.Errorf("%s requires %s; provide fixture services explicitly", s.Name, req)
			}
		}
		tests = append(tests, s)
	}
	if len(tests) == 0 {
		return nil, fmt.Errorf("no test_ functions found")
	}
	return tests, nil
}
func (r *Result) EmitGoTests() (string, error) {
	tests, err := r.Tests()
	if err != nil {
		return "", err
	}
	return r.emitGo(tests)
}
func (r *Result) EmitJSTests() (string, string, error) {
	tests, err := r.Tests()
	if err != nil {
		return "", "", err
	}
	js, decl, err := r.Emit(false)
	if err != nil {
		return "", "", err
	}
	js += "const __ef_tests = ["
	for _, test := range tests {
		js += "[" + quoted(test.Name) + ",__ef_function_" + test.Name + "],"
	}
	js += `];const __ef_results=[];let __ef_passed=true;
for(const [name,program] of __ef_tests){const exit=await Effect.runPromiseExit(Effect.provideService(program(),__ef_service_Assert,__ef_provider_Assertions));const passed=Exit.isSuccess(exit);__ef_passed&&=passed;__ef_results.push(passed?{name,passed}:{name,passed,cause:Cause.pretty(exit.cause),reasons:exit.cause.reasons.map(reason=>reason._tag==="Fail"?{kind:"failure",tag:reason.error?._tag??"Unknown",message:reason.error?.message??String(reason.error)}:reason._tag==="Die"?{kind:"defect",message:String(reason.defect)}:{kind:"interrupt",message:String(reason.fiberId??"interrupted")})});}
console.log(JSON.stringify({schemaVersion:1,passed:__ef_passed,tests:__ef_results}));if(!__ef_passed)process.exitCode=1;
`
	return js, decl, nil
}

// This is a conservative file-level capability check, not an OS sandbox.
func (r *Result) TestMode(live bool) error {
	if !r.Checked {
		return fmt.Errorf("tests require checked source")
	}
	if live {
		return nil
	}
	if r.Program.GoOnly {
		return fmt.Errorf("native host capabilities require test --live")
	}
	var block func(*Block) bool
	var expr func(*Expr) bool
	expr = func(e *Expr) bool {
		if e == nil {
			return false
		}
		if e.Kind == "name" && e.Text == "provider" && (e.Name == "LiveClock" || e.Name == "LiveEnv") {
			return true
		}
		if e.Kind == "timeout" {
			return true
		}
		if expr(e.Left) || expr(e.Right) || block(e.Then) || block(e.Else) {
			return true
		}
		for _, a := range e.Args {
			if expr(a) {
				return true
			}
		}
		for _, field := range e.Fields {
			if expr(field.Value) {
				return true
			}
		}
		for _, arm := range e.Arms {
			if block(arm.Body) {
				return true
			}
		}
		return false
	}
	block = func(b *Block) bool {
		if b != nil {
			for _, s := range b.Statements {
				if expr(s.Value) {
					return true
				}
			}
		}
		return false
	}
	for _, f := range r.Program.Functions {
		if block(f.Body) {
			return fmt.Errorf("live clock/environment or timeout requires test --live")
		}
	}
	for _, p := range r.Program.Providers {
		for _, f := range p.Methods {
			if block(f.Body) {
				return fmt.Errorf("live clock/environment or timeout requires test --live")
			}
		}
	}
	return nil
}
