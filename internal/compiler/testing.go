package compiler

import (
	"fmt"
	"strings"
)

// testHarnessProviders are the deterministic fixtures the generated harness
// binds for every case. Their services are the only implicit test
// requirements, and they are the harness roots of a test application plan.
var testHarnessProviders = []string{"Assertions", "TestClock", "TestScheduler", "TestSync"}

func (r *Result) testHarnessService(name string) bool {
	for _, providerName := range testHarnessProviders {
		if provider := r.checkedProviders[providerName]; provider != nil && provider.Service == name {
			return true
		}
	}
	return false
}

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
		if !s.Contract.Effect || s.Contract.Success != voidTypeName || len(s.Params) != 0 {
			return nil, fmt.Errorf("%s must be an effect function with no parameters returning void", s.Name)
		}
		for _, req := range s.Contract.Services {
			if !r.testHarnessService(req) {
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
	application, err := r.GoApplication(GoGenerationTest)
	if err != nil {
		return "", err
	}
	return string(application.Main), nil
}

func isLiveTestProvider(name string) bool {
	switch name {
	case "LiveClock", "LiveScheduler", "LiveEnv":
		return true
	default:
		return false
	}
}

// This is a conservative file-level capability check, not an OS sandbox.
func (r *Result) TestMode(live bool) error {
	if !r.Checked {
		return fmt.Errorf("tests require checked source")
	}
	if live {
		return nil
	}
	if r.Program.NativeHost {
		return fmt.Errorf("native host capabilities require test --live")
	}
	var block func(*Block) bool
	var expr func(*Expr) bool
	expr = func(e *Expr) bool {
		if e == nil {
			return false
		}
		if e.Kind == "name" && e.Text == "provider" && isLiveTestProvider(e.Name) {
			return true
		}
		found := false
		forEachExprChild(e, func(child *Expr) {
			if !found && expr(child) {
				found = true
			}
		})
		if found {
			return true
		}
		if block(e.Then) || block(e.Else) {
			return true
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
				if expr(s.Payload) {
					return true
				}
			}
		}
		return false
	}
	for _, f := range r.Program.Functions {
		if block(f.Body) {
			return fmt.Errorf("live clock/scheduler/environment requires test --live")
		}
	}
	for _, p := range r.Program.Providers {
		for _, f := range p.Methods {
			if block(f.Body) {
				return fmt.Errorf("live clock/scheduler/environment requires test --live")
			}
		}
	}
	for _, plan := range r.Layers {
		for _, node := range plan.Nodes {
			if isLiveTestProvider(node.Implementation) {
				return fmt.Errorf("live clock/scheduler/environment requires test --live")
			}
		}
	}
	return nil
}
