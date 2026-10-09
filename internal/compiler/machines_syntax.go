package compiler

// Machine is one `machine` declaration: a narrow construct that binds the
// initial state and ordinary named step, entry and completion functions into a
// canonical checked plan. Behavior stays in those functions; the declaration
// has no transition, guard, action or timer sub-language. It exists because the
// static Stay same-tag rule needs compiler knowledge of which parameter is the
// current state (docs/specs/state-machines.md#construct-admission-gate).
type Machine struct {
	Name     string
	Span     Span
	Bindings []*MachineBinding
}

// MachineBinding is one `initial`, `step`, `enter` or `complete` line, kept in
// source order. Initial carries a value; the others name a module function.
type MachineBinding struct {
	Kind     string
	Span     Span
	Name     string
	NameSpan Span
	Value    *Expr
}

const (
	machineInitial  = "initial"
	machineStep     = "step"
	machineEnter    = "enter"
	machineComplete = "complete"
)

func (m *Machine) binding(kind string) *MachineBinding {
	for _, binding := range m.Bindings {
		if binding.Kind == kind {
			return binding
		}
	}
	return nil
}

// machineStatechartWords are binding words other machine notations use. They
// are refused with the first-profile explanation instead of a generic syntax
// error, so an author learns where that behavior belongs.
var machineStatechartWords = map[string]bool{
	"states": true, "state": true, "on": true, "after": true, "always": true, "invoke": true,
	"entry": true, "exit": true, "parallel": true, "history": true, "guard": true, "guards": true,
	"transition": true, "transitions": true, "context": true, "actions": true, "action": true,
	"timeout": true, "final": true, "output": true, "postpone": true, "state_timeout": true, "event_timeout": true,
}

func (p *parser) machineDeclaration() *Machine {
	name := p.name()
	machine := &Machine{Name: name.text, Span: name.span}
	if p.peek().text == "<" {
		p.fail(p.peek(), "machine declarations take no type parameters; the bound functions name their state, event and output types")
	}
	p.expect("{")
	seen := map[string]bool{}
	for !p.accept("}") {
		head := p.take()
		switch head.text {
		case machineInitial, machineStep, machineEnter, machineComplete:
		default:
			if machineStatechartWords[head.text] {
				p.fail(head, "machine "+head.text+" is unsupported: a machine binds only initial, step, enter and complete; hierarchy, parallel regions, history, eventless or delayed transitions, postponement, guards and actions stay in ordinary functions")
			}
			p.fail(head, "expected a machine binding: initial, step, enter or complete")
		}
		if seen[head.text] {
			p.fail(head, "duplicate machine "+head.text+" binding")
		}
		seen[head.text] = true
		binding := &MachineBinding{Kind: head.text, Span: head.span}
		if head.text == machineInitial {
			binding.Value = p.expr(0)
		} else {
			bound := p.name()
			binding.Name, binding.NameSpan = bound.text, bound.span
			if next := p.peek().text; next == "." || next == "(" || next == "<" {
				p.fail(p.peek(), "machine "+head.text+" names a module function; write the behavior in that function")
			}
		}
		machine.Bindings = append(machine.Bindings, binding)
		p.accept(";")
	}
	return machine
}

// collectMachineBreaks puts each binding on its own line, in source order.
func collectMachineBreaks(layout *formatLayout, machine *Machine) {
	for _, binding := range machine.Bindings {
		if !layout.preserve[binding.Span.Line] {
			layout.breaks[binding.Span.Offset] = true
		}
		collectExpressionBreaks(layout, binding.Value)
	}
}
