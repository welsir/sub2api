//go:build unit

package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBalanceBillingPathsCheckWeeklyThresholdAfterDeduction(t *testing.T) {
	for _, filename := range []string{"gateway_usage_billing.go", "openai_gateway_usage.go"} {
		parsed, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		require.NoError(t, err, filename)

		calls := 0
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if ok && selector.Sel.Name == "CheckWeeklyCostAfterDeduction" {
				calls++
			}
			return true
		})
		require.Equalf(t, 1, calls, "%s must check the weekly threshold once after successful balance billing", filename)
	}
}
