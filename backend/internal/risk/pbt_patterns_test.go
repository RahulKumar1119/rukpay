package risk

import (
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"math/big"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// This file changes the package variable patternTable while it runs, and puts
// it back with t.Cleanup. That is safe only because no test of this package runs
// in parallel: Go runs the tests of one package one after the other unless a
// test calls t.Parallel. TestNoRiskTestRunsInParallel keeps it that way.

// replacePatternTable returns a function that sets the Pattern_Table, and
// registers a cleanup that puts the original table back when the test ends.
func replacePatternTable(tb testing.TB) func(table []FraudPattern) {
	tb.Helper()
	saved := patternTable
	tb.Cleanup(func() { patternTable = saved })
	return func(table []FraudPattern) { patternTable = table }
}

// fraudPattern returns the row for the reference pattern r, with a calm reason
// that is not one of the real table's.
func (r refPattern) fraudPattern() FraudPattern {
	return FraudPattern{
		ID:            r.ID,
		WindowSeconds: r.Window,
		MinSmallCount: r.MinSmall,
		SmallMax:      r.SmallMax,
		LargeMin:      r.LargeMin,
		Points:        r.Points,
		Reason:        "A rule added by a test applies to this payment.",
	}
}

// shiftAmount returns base plus d limited to the valid amounts, 1 to the largest
// int64.
func shiftAmount(base, d int64) int64 {
	return clampAmount(new(big.Int).Add(big.NewInt(base), big.NewInt(d)))
}

// patternEntries returns the pattern Contributions of an Explanation as model
// entries, in the order they appear.
func patternEntries(explanation []Contribution) []refEntry {
	var out []refEntry
	for _, c := range explanation {
		if strings.HasPrefix(c.Signal, "pattern_") {
			out = append(out, refEntry{Signal: c.Signal, Points: c.Points})
		}
	}
	return out
}

// genPatternInput draws a valid Input aimed at the pattern row. The payment
// amount sits one below, on and one above the large minimum. The snapshot holds
// up to MinSmall+3 small payments, mostly to the payment's recipient, with
// amounts one below, on and one above the small maximum and ages one below, on
// and one above the window, plus a few events that have nothing to do with the
// pattern. One case in eight holds 100 matching events, so that a pattern that
// multiplies its points by the number of matches is caught.
func genPatternInput(row refPattern) *rapid.Generator[Input] {
	return rapid.Custom(func(t *rapid.T) Input {
		ref := rapid.SampledFrom([]string{"", "r***@okaxis", "r***@okaxis", "r***@okaxis", "XXXX-XXXX-4821"}).Draw(t, "ref")
		now := genNow().Draw(t, "now")

		signals := genSignals().Draw(t, "signals")
		signals.AmountINR = rapid.OneOf(
			rapid.SampledFrom([]int64{shiftAmount(row.LargeMin, -1), row.LargeMin, row.LargeMin, shiftAmount(row.LargeMin, 1), 1, math.MaxInt64}),
			rapid.SampledFrom([]int64{shiftAmount(row.LargeMin, -1), row.LargeMin, row.LargeMin, shiftAmount(row.LargeMin, 1)}),
			genAmount(),
		).Draw(t, "amount")

		// Half of the cases are clean: every small payment matches, so the count
		// of matching events is exactly n and sits at the edge of the minimum.
		clean := rapid.Bool().Draw(t, "clean")
		n := rapid.SampledFrom([]int{0, row.MinSmall - 1, row.MinSmall - 1, row.MinSmall, row.MinSmall, row.MinSmall, row.MinSmall + 1, row.MinSmall + 3}).Draw(t, "small_count")
		if rapid.IntRange(0, 7).Draw(t, "many") == 0 {
			n = 100
		}
		events := make([]HistoryEvent, 0, n+4)
		for i := 0; i < n; i++ {
			e := HistoryEvent{BeneficiaryRef: ref}
			if clean {
				e.AmountINR = rapid.OneOf(
					rapid.SampledFrom([]int64{row.SmallMax, row.SmallMax, 1}),
					rapid.Int64Range(1, row.SmallMax),
				).Draw(t, "clean_amount")
				age := rapid.OneOf(
					rapid.SampledFrom([]int64{row.Window - 1, 0}),
					rapid.Int64Range(0, row.Window-1),
				).Draw(t, "clean_age")
				e.At = timeBefore(now, age)
				events = append(events, e)
				continue
			}
			if rapid.IntRange(0, 9).Draw(t, "other_ref") == 0 {
				e.BeneficiaryRef = drawEventRef(t, ref)
			}
			e.AmountINR = rapid.OneOf(
				rapid.SampledFrom([]int64{shiftAmount(row.SmallMax, -1), row.SmallMax, row.SmallMax, shiftAmount(row.SmallMax, 1), 1}),
				rapid.Int64Range(1, shiftAmount(row.SmallMax, 0)),
			).Draw(t, "small_amount")
			age := rapid.OneOf(
				rapid.SampledFrom([]int64{row.Window - 1, row.Window - 1, row.Window, row.Window + 1, 0}),
				rapid.Int64Range(0, row.Window-1),
			).Draw(t, "age")
			e.At = timeBefore(now, age)
			if rapid.IntRange(0, 15).Draw(t, "future") == 0 {
				e.At = timeAfter(now, rapid.Int64Range(1, 100).Draw(t, "future_seconds"))
			}
			events = append(events, e)
		}
		for i, noise := 0, rapid.IntRange(0, 3).Draw(t, "noise_count"); i < noise; i++ {
			events = append(events, drawEvent(t, now, ref, events))
		}
		sort.SliceStable(events, func(i, j int) bool { return events[i].At < events[j].At })

		return Input{Signals: signals, Now: now, History: events, BeneficiaryRef: ref}
	})
}

// requireModelPatternTable fails the test when the real Pattern_Table does not
// match the one the reference model holds. The model must be updated by hand
// when a row is added to the table, so that a row nobody wrote a model for cannot
// slip through the comparisons.
func requireModelPatternTable(tb testing.TB) {
	tb.Helper()
	real := Patterns()
	if len(real) != len(refPatternTable) {
		tb.Fatalf("the Pattern_Table has %d rows and the reference model %d: add the new row to refPatternTable",
			len(real), len(refPatternTable))
	}
	for i, r := range refPatternTable {
		p := real[i]
		if p.ID != r.ID || p.WindowSeconds != r.Window || p.MinSmallCount != r.MinSmall ||
			p.SmallMax != r.SmallMax || p.LargeMin != r.LargeMin || p.Points != r.Points {
			tb.Fatalf("Pattern_Table row %d = %+v, the reference model holds %+v", i, p, r)
		}
	}
}

// Feature: risk-intelligence, Property 8: Fraud patterns are table-driven and score once
//
// With the real Pattern_Table, a pattern Contribution exists exactly when the
// model says so, and carries the points of its row once, however many events
// qualify.
//
// **Validates: Requirements 11.3, 11.4, 11.5, 11.6, 11.8**
func TestFraudPatternMatchesTheReferenceModel(t *testing.T) {
	requireModelPatternTable(t)
	row := refPatternTable[0]

	rapid.Check(t, func(t *rapid.T) {
		in := genPatternInput(row).Draw(t, "input")
		a, err := Assess(in)
		if err != nil {
			t.Fatalf("Assess(%+v) returned an error for valid signals: %v", in, err)
		}

		got := patternEntries(a.Explanation)
		want := refPatternEntries(refPatternTable, in)
		if !equalEntries(got, want) {
			t.Fatalf("pattern Contributions = %v, reference model = %v", got, want)
		}
		if problem := refReasonProblem(a.Explanation, false); problem != "" {
			t.Fatal(problem)
		}
		// Requirement 11 criterion 6: at most one Contribution, and its points
		// are the points of the row.
		if len(got) > 1 {
			t.Fatalf("%d pattern Contributions, want at most 1", len(got))
		}
		for _, c := range a.Explanation {
			if c.Signal == "pattern_test_then_large" && c.Points != row.Points {
				t.Fatalf("pattern_test_then_large has %d points, want %d", c.Points, row.Points)
			}
		}
	})
}

// Feature: risk-intelligence, Property 8: Fraud patterns are table-driven and score once
//
// A row added to the Pattern_Table is scored with no change to the scoring code:
// the test appends a row with random parameters, and the whole Explanation, the
// pattern lines and the caps that the extra points can trigger, equals the model
// that holds the same two rows. The test puts the table back when it ends.
//
// **Validates: Requirements 11.3, 11.5, 11.6, 11.8**
func TestFraudPatternAddedToTheTableIsScoredWithNoCodeChange(t *testing.T) {
	requireModelPatternTable(t)
	set := replacePatternTable(t)
	original := Patterns()

	rapid.Check(t, func(t *rapid.T) {
		extra := refPattern{
			ID:       rapid.SampledFrom([]string{"extra_rule", "second_rule", "rule_9"}).Draw(t, "id"),
			Window:   rapid.OneOf(rapid.SampledFrom([]int64{1, 2, 60, 600, 3600, 86400}), rapid.Int64Range(1, 200_000)).Draw(t, "window"),
			MinSmall: rapid.SampledFrom([]int{1, 1, 2, 3, 4, 5, 6}).Draw(t, "min_small"),
			SmallMax: rapid.OneOf(
				rapid.SampledFrom([]int64{1, 2, 500, 1000, 5000, math.MaxInt64}),
				rapid.Int64Range(1, 100_000),
			).Draw(t, "small_max"),
			LargeMin: rapid.OneOf(
				rapid.SampledFrom([]int64{1, 2, 2000, 10000, math.MaxInt64}),
				rapid.Int64Range(1, 100_000),
			).Draw(t, "large_min"),
			Points: rapid.IntRange(1, 40).Draw(t, "points"),
		}
		table := append(append([]FraudPattern(nil), original...), extra.fraudPattern())
		models := append(append([]refPattern(nil), refPatternTable...), extra)
		set(table)

		in := genPatternInput(extra).Draw(t, "input")
		a, err := Assess(in)
		if err != nil {
			t.Fatalf("Assess(%+v) returned an error for valid signals: %v", in, err)
		}

		want := refAssess(models, in)
		var got []refEntry
		for _, c := range a.Explanation {
			got = append(got, refEntry{Signal: c.Signal, Points: c.Points})
		}
		if !equalEntries(got, want.Entries) {
			t.Fatalf("Explanation = %v, reference model = %v", got, want.Entries)
		}
		if a.Score != want.Score || a.Behaviour != want.Behaviour || a.State != want.State {
			t.Fatalf("Score, Behaviour, State = %d, %d, %q, reference model = %d, %d, %q",
				a.Score, a.Behaviour, a.State, want.Score, want.Behaviour, want.State)
		}

		if problem := refReasonProblem(a.Explanation, true); problem != "" {
			t.Fatal(problem)
		}

		// The reason of a pattern Contribution is the reason of its row.
		for _, c := range a.Explanation {
			if c.Signal == "pattern_"+extra.ID && c.Reason != table[len(table)-1].Reason {
				t.Fatalf("reason of %s = %q, want the reason of its row %q", c.Signal, c.Reason, table[len(table)-1].Reason)
			}
		}
	})
}

// TestFraudPatternTableHoldsTheRequiredEntry checks the entry of Requirement 11
// criterion 4, the shape of every id, and that Patterns returns a copy.
func TestFraudPatternTableHoldsTheRequiredEntry(t *testing.T) {
	table := Patterns()
	if len(table) == 0 {
		t.Fatal("the Pattern_Table is empty")
	}
	var found bool
	for _, p := range table {
		if p.ID != "test_then_large" {
			continue
		}
		found = true
		if p.WindowSeconds != 3600 || p.MinSmallCount != 3 || p.SmallMax != 1000 || p.LargeMin != 10000 || p.Points != 15 {
			t.Errorf("test_then_large = %+v, want window 3600, 3 small payments of at most 1000, large from 10000, 15 points", p)
		}
		if want := "Several small payments to this recipient were followed by a large one."; p.Reason != want {
			t.Errorf("test_then_large reason = %q, want %q", p.Reason, want)
		}
	}
	if !found {
		t.Error("the Pattern_Table has no test_then_large entry")
	}
	requireModelPatternTable(t)

	// A caller that changes the returned slice does not change the table.
	table[0].Points = 99
	table[0].ID = "changed"
	if again := Patterns(); again[0].Points == 99 || again[0].ID == "changed" {
		t.Errorf("Patterns() returned the table itself: changing the result changed the table to %+v", again[0])
	}
}

// TestNoRiskTestRunsInParallel fails when any test file of this package calls
// Parallel. The tests above replace the Pattern_Table, so no test of the package
// may run at the same time as another.
func TestNoRiskTestRunsInParallel(t *testing.T) {
	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("found no test file, so the check would pass with nothing to check")
	}
	fset := token.NewFileSet()
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Parallel" {
				t.Errorf("%s calls %s: the tests that replace the Pattern_Table must not run in parallel",
					fset.Position(call.Pos()), sel.Sel.Name)
			}
			return true
		})
	}
}
