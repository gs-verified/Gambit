// Command check verifies the two machine-checkable Code Olympics 2026
// constraints against a Go source tree:
//
//	Short-Name Ninja      every *variable* identifier is <= 3 characters
//	Professional Builder  <= 500 non-empty, non-comment lines per tool
//
// The line rule follows the official definition exactly: a line counts iff it
// holds at least one non-comment token (so blank lines, comment-only lines and
// the trailing auto-semicolon never inflate the count). The variable rule is
// applied to variables only -- declared vars, := bindings, parameters, named
// results, receivers and range/type-switch vars. Type names, function and
// method names, struct fields and constants are intentionally exempt, which is
// what lets the terse code stay readable.
//
// This file is project tooling, not a contest submission: it is excluded from
// every tool's budget and does not itself obey the 3-char rule.
//
// Usage:
//
//	go run . <dir>        # check one tool's directory (top-level .go files)
//	go -C tools run . ..  # from the gambit/ root: check the tool
package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const maxNameLen = 3 // the Short-Name Ninja ceiling
const lineBudget = 500

func main() {
	roots := os.Args[1:]
	if len(roots) == 0 {
		roots = []string{"."}
	}
	bad := false
	for _, root := range roots {
		ok, err := checkDir(root)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}
		if !ok {
			bad = true
		}
	}
	if bad {
		os.Exit(1)
	}
}

// checkDir reports on every .go file under root, returning false if the tool
// busts the line budget or contains an over-long variable name.
func checkDir(root string) (bool, error) {
	fset := token.NewFileSet()
	var viol []string
	code, test := 0, 0
	// Scan only this tool's own top-level .go files -- never nested directories
	// such as the Rust port or this checker itself, which aren't part of the
	// tool's budget. (A tool is a single directory of Go files.)
	ents, err := os.ReadDir(root)
	if err != nil {
		return false, err
	}
	for _, ent := range ents {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".go") {
			continue
		}
		path := filepath.Join(root, ent.Name())
		src, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}
		n := countCodeLines(fset, path, src)
		if strings.HasSuffix(path, "_test.go") {
			test += n
		} else {
			code += n
		}
		file, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			return false, err
		}
		viol = append(viol, longVars(fset, file)...)
	}
	sort.Strings(viol)
	fmt.Printf("== %s ==\n", root)
	for _, v := range viol {
		fmt.Println("  VAR>3  ", v)
	}
	fmt.Printf("  code lines (non-test): %d / %d\n", code, lineBudget)
	fmt.Printf("  test lines:            %d\n", test)
	fmt.Printf("  variable violations:   %d\n\n", len(viol))
	return code <= lineBudget && len(viol) == 0, nil
}

// countCodeLines counts lines bearing at least one non-comment token, matching
// the contest's "non-empty, non-comment lines of code" definition.
func countCodeLines(fset *token.FileSet, path string, src []byte) int {
	file := fset.AddFile(path, fset.Base(), len(src))
	var sc scanner.Scanner
	sc.Init(file, src, nil, scanner.ScanComments)
	seen := map[int]bool{}
	for {
		pos, tok, _ := sc.Scan()
		if tok == token.EOF {
			break
		}
		// Comments are free; auto-inserted semicolons never add a fresh line
		// of their own, so skip both.
		if tok == token.COMMENT || tok == token.SEMICOLON {
			continue
		}
		seen[file.Line(pos)] = true
	}
	return len(seen)
}

// longVars walks the AST and returns one message per variable identifier that
// exceeds maxNameLen.
func longVars(fset *token.FileSet, file *ast.File) []string {
	var out []string
	flag := func(id *ast.Ident) {
		if id == nil || id.Name == "_" || len(id.Name) <= maxNameLen {
			return
		}
		at := fset.Position(id.Pos())
		out = append(out, fmt.Sprintf("%s:%d:%d  %q (%d chars)",
			filepath.Base(at.Filename), at.Line, at.Column, id.Name, len(id.Name)))
	}
	fields := func(list *ast.FieldList) {
		if list == nil {
			return
		}
		for _, fld := range list.List {
			for _, id := range fld.Names {
				flag(id)
			}
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch x := node.(type) {
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE {
				for _, lhs := range x.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						flag(id)
					}
				}
			}
		case *ast.GenDecl:
			if x.Tok == token.VAR { // const/type/import are exempt
				for _, sp := range x.Specs {
					if vs, ok := sp.(*ast.ValueSpec); ok {
						for _, id := range vs.Names {
							flag(id)
						}
					}
				}
			}
		case *ast.FuncDecl:
			fields(x.Recv) // receiver is a variable
			if x.Type != nil {
				fields(x.Type.Params)
				fields(x.Type.Results)
			}
		case *ast.FuncLit:
			if x.Type != nil {
				fields(x.Type.Params)
				fields(x.Type.Results)
			}
		case *ast.RangeStmt:
			if id, ok := x.Key.(*ast.Ident); ok {
				flag(id)
			}
			if id, ok := x.Value.(*ast.Ident); ok {
				flag(id)
			}
		case *ast.TypeSwitchStmt:
			if as, ok := x.Assign.(*ast.AssignStmt); ok && as.Tok == token.DEFINE {
				for _, lhs := range as.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						flag(id)
					}
				}
			}
		}
		return true
	})
	return out
}
