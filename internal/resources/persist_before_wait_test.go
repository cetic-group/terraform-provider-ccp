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

// TestEarlyStateWriteDoesNotClobberThePlan guards the regression that shipped in
// v6.6.2. The early write projects the create response into a model — and if it
// projects into `plan` itself, it overwrites what the practitioner asked for
// with what the backend currently reports. Code further down reads `plan` to
// decide follow-up calls, so that intent must survive.
//
// `ccp_vnet` is the case that broke: isolation is not accepted by POST /vnets,
// it needs a dedicated /firewall/isolation call, and the decision to make that
// call reads plan.Isolated. Projecting the create response into `plan` set it
// back to false, the toggle never fired, and the apply ended on "Provider
// produced inconsistent result after apply: .isolated: was cty.True, but now
// cty.False".
//
// The invariant: the state write that precedes the wait must target a copy.
func TestEarlyStateWriteDoesNotClobberThePlan(t *testing.T) {
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

			firstWait := token.NoPos
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || firstWait.IsValid() {
					return true
				}
				var b strings.Builder
				if err := printer.Fprint(&b, fset, call.Fun); err != nil {
					return true
				}
				for _, w := range waitCalls {
					if b.String() == w {
						firstWait = call.Pos()
					}
				}
				return true
			})
			if !firstWait.IsValid() {
				continue
			}

			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || call.Pos() > firstWait {
					return true
				}
				var b strings.Builder
				if err := printer.Fprint(&b, fset, call); err != nil {
					return true
				}
				if strings.HasPrefix(b.String(), "resp.State.Set(ctx, &plan)") {
					t.Errorf(
						"%s: the state write at %s targets `plan` itself. Project the create "+
							"response into a copy — later code reads `plan` for the practitioner's "+
							"intent, and overwriting it drops that intent silently (see v6.6.2)",
						file, fset.Position(call.Pos()))
				}
				return true
			})
		}
	}
}
