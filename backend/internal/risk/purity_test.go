package risk

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// clockImportReason says why the Risk_Engine may not import path, or returns ""
// when it may. Reading no clock means calling no function that returns the
// current time, and using no randomness means the same inputs always give the
// same result (Requirement 9 criteria 5 and 10). imports_test.go bans the
// packages that do I/O. This list bans the ones that read the clock or give
// random numbers.
func clockImportReason(path string) string {
	switch path {
	case "time":
		return "a package that reads the clock"
	case "math/rand", "math/rand/v2", "crypto/rand":
		return "a source of randomness"
	}
	return ""
}

// TestRiskNonTestFilesImportNoClockOrRandomness checks that no non-test Go file
// of the risk package imports the time package or a random number package, so the
// engine is a pure function of its argument. It fails when the directory has no
// Go file, so it cannot pass with nothing to check.
func TestRiskNonTestFilesImportNoClockOrRandomness(t *testing.T) {
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
		t.Fatal("no non-test Go file in the risk package, so the check would pass without checking anything")
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
			if why := clockImportReason(path); why != "" {
				t.Errorf("%s imports %q, %s", file, path, why)
			}
		}
	}
}

// TestClockImportBanListCatchesKnownPackages guards the check above.
func TestClockImportBanListCatchesKnownPackages(t *testing.T) {
	for _, path := range []string{"time", "math/rand", "math/rand/v2", "crypto/rand"} {
		if clockImportReason(path) == "" {
			t.Errorf("clockImportReason(%q) is empty, want a reason", path)
		}
	}
	for _, path := range []string{"sort", "strings", "math", "math/big", "errors", "timeutil", "random"} {
		if why := clockImportReason(path); why != "" {
			t.Errorf("clockImportReason(%q) = %q, want none", path, why)
		}
	}
}
