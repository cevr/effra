package compiler

import "strings"

// The distributed effra/i64 module converts between i64 and decimal text.
// Its members are ordinary bundled functions with real signatures in
// bundled/i64/*.ef; only the body is compiler-owned. Each source body is the
// single marker string "effra:intrinsic <operation>", which the loader and
// the interface producer replace with an "intrinsic" expression over the
// function's parameter. Source text cannot spell that expression kind, so
// the operations exist only behind these signatures.
//
// format is pure and total. parse is an effect operation: text that does not
// match [+-]?[0-9]+ (Go's base-10 strconv grammar) is a syntax failure, and
// a value outside the signed 64-bit range is a range failure. Syntax is
// classified before range on both targets, and the failure message never
// echoes the input.

const (
	i64TextModule     = "effra/i64"
	i64FormatOp       = "i64.format"
	i64ParseOp        = "i64.parse"
	i64ParseFailure   = "I64ParseFailure"
	i64SyntaxMessage  = "invalid i64 syntax"
	i64RangeMessage   = "i64 out of range"
	intrinsicMarkerAt = "effra:intrinsic "
)

// admitBundledIntrinsic replaces a compiler-owned body marker with its
// operation. It reports false when the source does not have exactly the
// marker the index declares, so a distributed source cannot drift from the
// operation the emitters implement.
func admitBundledIntrinsic(operation string, f *Function) bool {
	if operation == "" {
		return true
	}
	if f == nil || len(f.Params) != 1 || f.Body == nil || len(f.Body.Statements) != 1 {
		return false
	}
	statement := f.Body.Statements[0]
	marker := statement.Value
	if statement.Kind != "expr" || marker == nil || marker.Kind != "string" || marker.Text != intrinsicMarkerAt+operation {
		return false
	}
	statement.Value = &Expr{Kind: "intrinsic", Name: operation, Left: &Expr{Kind: "name", Name: f.Params[0].Name, Span: marker.Span, Extent: marker.Extent}, Span: marker.Span, Extent: marker.Extent}
	return true
}

// intrinsicOperation checks a compiler-owned body. format yields a string;
// parse executes now, inside its function's recipe, yielding an i64 and
// contributing only its named failure.
func (c *checker) intrinsicOperation(e *Expr, env localEnv) checkedExpression {
	operand := c.expr(e.Left, env, false)
	switch e.Name {
	case i64FormatOp:
		if operand.isEffect() || !c.sameType(operand, "i64") {
			c.diagnostic("EF106", "i64.format requires an i64", e.Span)
		}
		return c.checkedData("string")
	case i64ParseOp:
		if operand.isEffect() || !c.sameType(operand, "string") {
			c.diagnostic("EF106", "i64.parse requires a string", e.Span)
		}
		t := c.checkedData("i64")
		t.evaluation = c.evaluation(c.internRow([]string{i64ParseFailure}), emptyRowID)
		c.reasons = append(c.reasons, Contribution{"failure", []string{i64ParseFailure}, e.Span})
		return t
	}
	c.diagnostic("EF103", "unsupported compiler-owned operation "+e.Name, e.Span)
	return c.checkedData("invalid")
}

// goI64TextHelpers are emitted only when the plan retains the operation.
// Both need strconv, which the generated main imports only then.
var goI64TextHelpers = []struct{ name, source string }{
	{i64FormatOp, "func efI64Format(value int64)string{return strconv.FormatInt(value,10)}\n"},
	{i64ParseOp, `func efI64Parse(text string)efExit[int64]{digits:=text;if len(digits)>0&&(digits[0]=='+'||digits[0]=='-'){digits=digits[1:]};if digits==""{return er.Fail[int64](` + quotedGo(i64ParseFailure) + `,` + quotedGo(i64SyntaxMessage) + `)};for index:=0;index<len(digits);index++{if digits[index]<'0'||digits[index]>'9'{return er.Fail[int64](` + quotedGo(i64ParseFailure) + `,` + quotedGo(i64SyntaxMessage) + `)}};value,err:=strconv.ParseInt(text,10,64);if err!=nil{return er.Fail[int64](` + quotedGo(i64ParseFailure) + `,` + quotedGo(i64RangeMessage) + `)};return er.Succeed(value)}
`},
}

func quotedGo(text string) string { return `"` + strings.ReplaceAll(text, `"`, `\"`) + `"` }

func (p *ApplicationPlan) requiresStrconv() bool {
	for _, helper := range goI64TextHelpers {
		if p.Requires(RequiresHelper, helper.name) {
			return true
		}
	}
	return false
}

func (g *goEmitter) intrinsicOperation(e *Expr, ret string, out *strings.Builder) string {
	operand := g.expr(e.Left, false, ret, out)
	if e.Name == i64FormatOp {
		return "efI64Format(" + operand + ")"
	}
	name := g.temp()
	out.WriteString(name + " := efI64Parse(" + operand + ")\n" + g.failed(name, ret))
	return name + ".Value"
}

const jsI64FormatHelper = "const __ef_i64Format = value => String(value);\n"

const jsI64ParseHelper = `const __ef_i64Parse = text => Effect.suspend(() => {
  if (!/^[+-]?[0-9]+$/.test(text)) return Effect.fail({ _tag: "` + i64ParseFailure + `", message: "` + i64SyntaxMessage + `" });
  const value = BigInt(text);
  return value < -9223372036854775808n || value > 9223372036854775807n ? Effect.fail({ _tag: "` + i64ParseFailure + `", message: "` + i64RangeMessage + `" }) : Effect.succeed(value);
});
`

func jsIntrinsicOperation(e *Expr) string {
	operand := jsExpr(e.Left, false)
	if e.Name == i64FormatOp {
		return "__ef_i64Format(" + operand + ")"
	}
	return "(yield* __ef_i64Parse(" + operand + "))"
}
