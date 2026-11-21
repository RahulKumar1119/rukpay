package store

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// importsBannedLoggers lists import paths and path prefixes of logging
// packages. The store and risk packages log nothing, so they must not import
// any of these (Requirement 12.5).
func importsBannedLoggers() []string {
	return []string{
		"log",
		"log/slog",
		"github.com/sirupsen/logrus",
		"go.uber.org/zap",
		"github.com/rs/zerolog",
		"github.com/golang/glog",
		"k8s.io/klog",
		"github.com/apex/log",
		"github.com/charmbracelet/log",
		"github.com/go-kit/log",
		"github.com/hashicorp/go-hclog",
		"github.com/op/go-logging",
		"github.com/inconshreveable/log15",
		"github.com/rs/xlog",
	}
}

// importsOf returns every import path of every Go file in dir, including test
// files, keyed by import path with the files that import it.
func importsOf(t *testing.T, dir string) map[string][]string {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}
	if len(files) == 0 {
		t.Fatalf("no Go files in %s, so the import check would pass without checking anything", dir)
	}

	fset := token.NewFileSet()
	imports := make(map[string][]string)
	for _, file := range files {
		f, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, spec := range f.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("%s: bad import path %s: %v", file, spec.Path.Value, err)
			}
			imports[path] = append(imports[path], filepath.Base(file))
		}
	}
	return imports
}

// importsIsBanned reports why path is not allowed, or "" when it is allowed.
func importsIsBanned(path string) string {
	if strings.Contains(strings.ToLower(path), "sqlite") {
		return "a SQLite package"
	}
	for _, banned := range importsBannedLoggers() {
		if path == banned || strings.HasPrefix(path, banned+"/") {
			return "a logging package"
		}
	}
	return ""
}

// TestPackagesImportNoSQLiteOrLogger parses the imports of the store and risk
// packages and fails on a SQLite package or a logger (Requirements 8.6, 12.5).
func TestPackagesImportNoSQLiteOrLogger(t *testing.T) {
	for _, dir := range []string{".", filepath.Join("..", "risk")} {
		dir := dir
		t.Run(filepath.Base(mustAbs(t, dir)), func(t *testing.T) {
			for path, files := range importsOf(t, dir) {
				if why := importsIsBanned(path); why != "" {
					t.Errorf("%s imports %q, %s", strings.Join(files, ", "), path, why)
				}
			}
		})
	}
}

// TestImportsBanListCatchesKnownPackages guards the check above: the ban list
// must flag the packages it is written for and allow the ones the code uses.
func TestImportsBanListCatchesKnownPackages(t *testing.T) {
	tests := []struct {
		path   string
		banned bool
	}{
		{path: "log", banned: true},
		{path: "log/slog", banned: true},
		{path: "github.com/sirupsen/logrus", banned: true},
		{path: "go.uber.org/zap", banned: true},
		{path: "go.uber.org/zap/zapcore", banned: true},
		{path: "github.com/rs/zerolog/log", banned: true},
		{path: "github.com/mattn/go-sqlite3", banned: true},
		{path: "modernc.org/sqlite", banned: true},
		{path: "gorm.io/driver/sqlite", banned: true},
		{path: "database/sql", banned: false},
		{path: "logger/other", banned: false},
		{path: "context", banned: false},
		{path: "pgregory.net/rapid", banned: false},
		{path: "github.com/aws/aws-sdk-go-v2/service/dynamodb", banned: false},
	}
	for _, tt := range tests {
		got := importsIsBanned(tt.path) != ""
		if got != tt.banned {
			t.Errorf("importsIsBanned(%q) = %v, want %v", tt.path, got, tt.banned)
		}
	}
}

// TestModuleFilesMentionNoSQLite checks the module files for a SQLite
// dependency, which would also catch one that only a tool or test imports
// (Requirement 8.6).
func TestModuleFilesMentionNoSQLite(t *testing.T) {
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join("..", "..", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if strings.Contains(strings.ToLower(string(data)), "sqlite") {
			t.Errorf("%s mentions sqlite", name)
		}
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
