package risk

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// ioLoggerPaths lists the logging packages that store/imports_test.go bans, as
// import paths and path prefixes. The list is copied here because a test of one
// package cannot import a helper from another package's tests.
var ioLoggerPaths = []string{
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

// ioImportReason says why the Risk_Engine may not import path, or returns "" when
// it may (Requirements 9.10 and 27.12). The store package is allowed: the engine
// imports it for types and constants only.
func ioImportReason(path string) string {
	switch path {
	case "os", "io", "io/ioutil", "io/fs", "net", "os/exec", "syscall", "database/sql":
		return "an I/O package"
	}
	if strings.HasPrefix(path, "net/") {
		return "a network package"
	}
	if strings.HasPrefix(path, "github.com/aws/") {
		return "an AWS package"
	}
	for _, logger := range ioLoggerPaths {
		if path == logger || strings.HasPrefix(path, logger+"/") {
			return "a logging package"
		}
	}
	return ""
}

// TestRiskNonTestFilesImportNoIOPackage checks that no non-test Go file of the
// risk package imports a package that performs I/O or logging, so the engine
// stays free of I/O (Requirements 9.10, 27.12). It fails when the directory has
// no Go file, so it cannot pass with nothing to check.
func TestRiskNonTestFilesImportNoIOPackage(t *testing.T) {
	all, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	var files []string
	for _, f := range all {
		if !strings.HasSuffix(f, "_test.go") {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		t.Fatal("no non-test Go file in the risk package, so the import check would pass without checking anything")
	}

	fset := token.NewFileSet()
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
			if why := ioImportReason(path); why != "" {
				t.Errorf("%s imports %q, %s", file, path, why)
			}
		}
	}
}

// TestIOImportBanListCatchesKnownPackages guards the check above: the list must
// flag the packages it is written for and allow the ones the engine uses.
func TestIOImportBanListCatchesKnownPackages(t *testing.T) {
	tests := []struct {
		path   string
		banned bool
	}{
		{"os", true},
		{"io", true},
		{"io/ioutil", true},
		{"io/fs", true},
		{"net", true},
		{"net/http", true},
		{"net/url", true},
		{"os/exec", true},
		{"syscall", true},
		{"database/sql", true},
		{"log", true},
		{"log/slog", true},
		{"github.com/aws/aws-sdk-go-v2/service/dynamodb", true},
		{"github.com/aws/aws-lambda-go/lambda", true},
		{"github.com/sirupsen/logrus", true},
		{"go.uber.org/zap/zapcore", true},
		{"github.com/rs/zerolog/log", true},
		{"github.com/RahulKumar1119/rukpay/backend/internal/store", false},
		{"strings", false},
		{"math", false},
		{"sort", false},
		{"errors", false},
		{"logger/other", false},
		{"network", false},
		{"ioutil", false},
	}
	for _, tt := range tests {
		if got := ioImportReason(tt.path) != ""; got != tt.banned {
			t.Errorf("ioImportReason(%q) banned = %v, want %v", tt.path, got, tt.banned)
		}
	}
}
