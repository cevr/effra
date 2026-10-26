package compiler

import (
	"strings"
)

func emittedTypeVariable(n *semanticTypeNode) string {
	if strings.HasPrefix(n.Declaration, "type-parameter:template:") {
		return "__ef_type_" + n.Name
	}
	return n.Name
}
