//go:build unit

package handler

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsageRecordInputsCarryWorkingDirectorySnapshot(t *testing.T) {
	files := []string{
		"gateway_handler.go",
		"gateway_handler_chat_completions.go",
		"gateway_handler_responses.go",
		"openai_chat_completions.go",
		"openai_gateway_handler.go",
	}

	total := 0
	for _, filename := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		require.NoError(t, err, filename)

		ast.Inspect(parsed, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok || !isUsageRecordInputType(literal.Type) {
				return true
			}
			total++
			require.Truef(t, compositeLiteralHasField(literal, "WorkingDirectory"), "%s usage input must carry the extracted working-directory scalar", filename)
			return true
		})
	}

	require.Equal(t, 8, total)
}

func isUsageRecordInputType(expr ast.Expr) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || pkg.Name != "service" {
		return false
	}
	return selector.Sel.Name == "RecordUsageInput" || selector.Sel.Name == "OpenAIRecordUsageInput"
}

func compositeLiteralHasField(literal *ast.CompositeLit, name string) bool {
	for _, element := range literal.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		ident, ok := field.Key.(*ast.Ident)
		if ok && ident.Name == name {
			return true
		}
	}
	return false
}
