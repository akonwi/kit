package protocol

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

type stringEnum interface{ EnumValues() []string }

func TestStringEnumValuesCoverDeclaredConstants(t *testing.T) {
	t.Parallel()
	enums := map[string]stringEnum{"ScratchpadErrorCode": ScratchpadErrorCode(""), "VCSHeadKind": VCSHeadKind("")}
	files, err := parser.ParseDir(token.NewFileSet(), ".", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	declared := make(map[string]map[string]bool)
	for _, file := range files["protocol"].Files {
		ast.Inspect(file, func(node ast.Node) bool {
			decl, ok := node.(*ast.GenDecl)
			if !ok || decl.Tok != token.CONST {
				return true
			}
			var inherited ast.Expr
			for _, specNode := range decl.Specs {
				spec := specNode.(*ast.ValueSpec)
				if spec.Type != nil {
					inherited = spec.Type
				}
				ident, ok := inherited.(*ast.Ident)
				if !ok || enums[ident.Name] == nil {
					continue
				}
				for _, expression := range spec.Values {
					literal, ok := expression.(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}
					value, err := strconv.Unquote(literal.Value)
					if err != nil {
						t.Fatal(err)
					}
					if declared[ident.Name] == nil {
						declared[ident.Name] = make(map[string]bool)
					}
					declared[ident.Name][value] = true
				}
			}
			return false
		})
	}
	for name, enum := range enums {
		values := make(map[string]bool)
		for _, value := range enum.EnumValues() {
			values[value] = true
		}
		if len(values) != len(declared[name]) {
			t.Fatalf("%s values = %v, declared = %v", name, values, declared[name])
		}
		for value := range declared[name] {
			if !values[value] {
				t.Errorf("%s omits %q", name, value)
			}
		}
	}
}
