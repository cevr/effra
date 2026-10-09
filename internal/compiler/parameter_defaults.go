package compiler

// refuseUnadmittedParameterContract keeps the formatter-only grammar
// checkpoint from publishing checked facts before their public contract and
// interface identities are available.
func (c *checker) refuseUnadmittedParameterContract(p *Param) {
	if p.requiredChoice {
		c.diagnostic("EF127", "required-choice parameter syntax is not yet admitted by the checker", p.requiredSpan)
	}
	if p.defaultExpr != nil {
		c.diagnostic("EF127", "parameter defaults are not yet admitted by the checker", p.defaultSpan)
	}
}
