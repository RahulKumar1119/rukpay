package vault

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Smoke tests on the source of the vault, guardian and httpapi packages: no SQLite
// package anywhere, and the vault package starts no goroutine, schedules no timer
// and imports no logger (Requirements 4.3, 11.7).

// packageFile is one parsed Go file.
type packageFile struct {
	name string
	test bool
	ast  *ast.File
}

// parseDir parses every Go file of the package in dir, test files included.
func parseDir(t *testing.T, dir string) []packageFile {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}
	if len(paths) == 0 {
		t.Fatalf("no Go files in %s, so the check would pass without checking anything", dir)
	}

	fset := token.NewFileSet()
	files := make([]packageFile, 0, len(paths))
	for _, path := range paths {
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		files = append(files, packageFile{
			name: filepath.Base(path),
			test: strings.HasSuffix(path, "_test.go"),
			ast:  f,
		})
	}
	return files
}

// importsOf returns the import paths of a file.
func importsOf(t *testing.T, f packageFile) []string {
	t.Helper()
	var paths []string
	for _, spec := range f.ast.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("%s: bad import path %s: %v", f.name, spec.Path.Value, err)
		}
		paths = append(paths, path)
	}
	return paths
}

func TestNoPackageImportsSQLite(t *testing.T) {
	for _, dir := range []string{".", filepath.Join("..", "guardian"), filepath.Join("..", "httpapi"), filepath.Join("..", "redact")} {
		dir := dir
		t.Run(filepath.Base(mustAbs(t, dir)), func(t *testing.T) {
			for _, f := range parseDir(t, dir) {
				for _, path := range importsOf(t, f) {
					if strings.Contains(strings.ToLower(path), "sqlite") {
						t.Errorf("%s imports %q, a SQLite package", f.name, path)
					}
				}
			}
		})
	}
}

// The vault package imports no logger. Its test files are checked too.
func TestVaultImportsNoLogger(t *testing.T) {
	banned := map[string]bool{
		"log":                        true,
		"log/slog":                   true,
		"github.com/sirupsen/logrus": true,
		"go.uber.org/zap":            true,
		"github.com/rs/zerolog":      true,
		"github.com/golang/glog":     true,
	}
	for _, f := range parseDir(t, ".") {
		for _, path := range importsOf(t, f) {
			if banned[path] {
				t.Errorf("%s imports %q, a logging package", f.name, path)
			}
		}
	}
}

// The vault package expires a vault only when it is read or acted on. Its source
// has no go statement and no time.AfterFunc, so no goroutine or timer can expire a
// vault in the background (Requirement 4.3). Test files are left out: the
// concurrency test starts goroutines on purpose.
func TestVaultSourceStartsNoGoroutineAndNoTimer(t *testing.T) {
	checked := 0
	for _, f := range parseDir(t, ".") {
		if f.test {
			continue
		}
		checked++
		ast.Inspect(f.ast, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.GoStmt:
				t.Errorf("%s has a go statement", f.name)
			case *ast.SelectorExpr:
				if id, ok := n.X.(*ast.Ident); ok && id.Name == "time" {
					switch n.Sel.Name {
					case "AfterFunc", "NewTimer", "NewTicker", "Tick", "After":
						t.Errorf("%s uses time.%s, a timer", f.name, n.Sel.Name)
					}
				}
			}
			return true
		})
	}
	if checked < 5 {
		t.Fatalf("checked only %d source files, so the check would be vacuous", checked)
	}
}

// TestSmokeChecksSeeTheirTargets guards the checks above: the source scan must
// find the constructs it looks for in a snippet that has them.
func TestSmokeChecksSeeTheirTargets(t *testing.T) {
	src := `package x
import ("time"; "log")
func f() { go g(); time.AfterFunc(time.Second, g); log.Println() }
func g() {}`
	file, err := parser.ParseFile(token.NewFileSet(), "x.go", src, 0)
	if err != nil {
		t.Fatalf("parse snippet: %v", err)
	}

	var found []string
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.GoStmt:
			found = append(found, "go")
		case *ast.SelectorExpr:
			if id, ok := n.X.(*ast.Ident); ok && id.Name == "time" && n.Sel.Name == "AfterFunc" {
				found = append(found, "time.AfterFunc")
			}
		}
		return true
	})
	sort.Strings(found)
	if strings.Join(found, ",") != "go,time.AfterFunc" {
		t.Errorf("the scan found %v in the snippet, want go and time.AfterFunc", found)
	}
}

func mustAbs(t *testing.T, dir string) string {
	t.Helper()
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("abs %s: %v", dir, err)
	}
	return abs
}
