package compiler

import (
	"fmt"
	"strings"
)

func emittedTypeVariable(n *semanticTypeNode) string {
	if strings.HasPrefix(n.Declaration, "type-parameter:template:") {
		return "__ef_type_" + n.Name
	}
	return n.Name
}

// Target declarations consume the checked canonical layout, including nested
// applications and parameter ownership; source spelling is not type authority.
func canonicalJSDataType(c *checker, id TypeID, declarations map[string]Declaration) string {
	n := c.node(id)
	switch n.Kind {
	case "type-variable":
		return emittedTypeVariable(n)
	case "application":
		r := c.templates[n.Declaration]
		args := []string{}
		for _, arg := range n.Args {
			args = append(args, canonicalJSDataType(c, arg, declarations))
		}
		return "__ef_template_" + r.EmissionName + "<" + strings.Join(args, ",") + ">"
	case "callable", "callable-shape":
		args := []string{}
		for i, arg := range n.Args {
			args = append(args, fmt.Sprintf("arg%d: %s", i, canonicalJSDataType(c, arg, declarations)))
		}
		result := canonicalJSDataType(c, n.Result, declarations)
		if n.Mode == "effect" {
			if n.Kind == "callable-shape" {
				result = "Effect.Effect<" + result + ", unknown, unknown>"
			} else {
				result = jsRowsContract(result, c.rowLabels(n.FailureRow), c.rowLabels(n.ServiceRow), declarations)
			}
		}
		return "(" + strings.Join(args, ",") + ") => " + result
	default:
		return jsValueType(n.Name)
	}
}
