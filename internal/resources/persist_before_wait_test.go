// Package resources_test carries invariants that must hold across every
// resource package, and that no per-resource unit test can enforce on its own.
package resources_test

import (
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// waitCalls are the helpers that block until a remote provisioning settles.
var waitCalls = []string{
	"pollUntilReady", "pollUntilActive", "pollForActive", "pollForStatus",
	"pollUntilRunningWithIP", "client.Poll",
}

// TestCreatePersistsStateBeforeWaiting guards the contract broken in #82: once
// the create API call returns, the remote object exists. If the provider then
// waits for provisioning and returns on failure without having written the
// state, Terraform holds no record of the object — it is neither destroyed nor
// re-planned, and the next apply strands it and creates a second one.
//
// The check is deliberately made on the source rather than through a mocked
// apply: it covers every resource at once, including those added later.
func TestCreatePersistsStateBeforeWaiting(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("*", "*.go"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}

		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "Create" || fn.Recv == nil || fn.Body == nil {
				continue
			}

			firstWait, firstSet := token.NoPos, token.NoPos
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				var b strings.Builder
				if err := printer.Fprint(&b, fset, call.Fun); err != nil {
					return true
				}
				name := b.String()
				if name == "resp.State.Set" && !firstSet.IsValid() {
					firstSet = call.Pos()
				}
				for _, w := range waitCalls {
					if name == w && !firstWait.IsValid() {
						firstWait = call.Pos()
					}
				}
				return true
			})

			if !firstWait.IsValid() {
				continue // création synchrone : rien à garantir
			}
			if !firstSet.IsValid() || firstSet > firstWait {
				t.Errorf(
					"%s: Create waits for provisioning at %s without having written the "+
						"state first — a provisioning failure would strand the remote object "+
						"outside Terraform (see issue #82)",
					file, fset.Position(firstWait))
			}
		}
	}
}
