package compiler

import "strings"

// checkParameterContract publishes only checked caller-choice facts. Defaults
// are compile-time scalar values; the source expression stays on Param for
// formatting and lexical provenance, while backends continue to receive the
// complete declared parameter vector.
func (c *checker) checkParameterContract(f *Function, p *Param) {
	p.RequiredChoice = p.requiredChoice
	p.DefaultValue = nil
	if p.requiredChoice && p.defaultExpr != nil {
		c.diagnostic("EF102", "a required-choice parameter cannot have a default", p.defaultSpan)
		return
	}
	if p.defaultExpr == nil {
		return
	}
	if strings.HasPrefix(f.Owner, "provider:") {
		c.diagnostic("EF127", "implementation methods cannot declare parameter defaults; the service declaration owns the call contract", p.defaultSpan)
		return
	}
	module := f.Module
	if module == "" {
		module = currentModuleIdentity
	}
	previousFacts := c.recordFacts
	c.recordFacts = true
	value, ok := c.constantExpression(module, p.defaultExpr, map[*Constant]uint8{})
	c.recordFacts = previousFacts
	if !ok {
		return
	}
	if value.Kind != p.Type {
		c.diagnostic("EF106", "parameter default must be "+p.Type, p.defaultSpan)
		return
	}
	copy := *value
	p.DefaultValue = &copy
}
