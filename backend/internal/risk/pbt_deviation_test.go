package risk

import (
	"math"
	"math/big"
	"testing"

	"pgregory.net/rapid"
)

// deviationCase is one generated case for the amount deviation rule: the amount
// of the payment and the snapshot it is compared with.
type deviationCase struct {
	amount  int64
	largest int64
	history []HistoryEvent
}

// clampAmount returns v limited to the valid amounts, 1 to the largest int64.
func clampAmount(v *big.Int) int64 {
	if v.Sign() < 1 {
		return 1
	}
	if !v.IsInt64() {
		return math.MaxInt64
	}
	return v.Int64()
}

// genLargestAmount draws the largest amount of the snapshot: the edges of int64
// where 3 times and 10 times the value overflow, small values, and the range.
func genLargestAmount() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{
			1, 2, 3, 333, 334, 666, 667, 700, 1000, 1001, 2000, 5000, 66_667, 1_000_000,
			math.MaxInt64 / 10, math.MaxInt64/10 + 1, math.MaxInt64 / 3, math.MaxInt64/3 + 1,
			math.MaxInt64 - 1, math.MaxInt64,
		}),
		rapid.Int64Range(1, 10000),
		rapid.Int64Range(1, math.MaxInt64),
	)
}

// genDeviationCase draws a payment amount and a snapshot. The amount sits one
// below, on and one above 1, 2, 3 and 10 times the largest amount, clamped to
// the valid range, or on one of the other edges: 1999, 2000, 2001 and the
// largest amount. The snapshot holds 0 to 100 events, with 4 and 5 drawn often,
// each of 1 up to the largest amount, and at least one equal to it.
func genDeviationCase() *rapid.Generator[deviationCase] {
	return rapid.Custom(func(t *rapid.T) deviationCase {
		largest := genLargestAmount().Draw(t, "largest")

		var amount int64
		switch rapid.IntRange(0, 9).Draw(t, "amount_kind") {
		case 0:
			amount = rapid.SampledFrom([]int64{1, 1999, 2000, 2001, 2999, 3000, 9999, 10000, math.MaxInt64}).Draw(t, "edge_amount")
		case 1:
			amount = rapid.Int64Range(1, math.MaxInt64).Draw(t, "any_amount")
		default:
			k := rapid.SampledFrom([]int64{1, 2, 3, 3, 3, 10, 10, 10}).Draw(t, "multiple")
			d := rapid.SampledFrom([]int64{-1, 0, 1}).Draw(t, "offset")
			v := new(big.Int).Mul(big.NewInt(k), big.NewInt(largest))
			amount = clampAmount(v.Add(v, big.NewInt(d)))
		}

		n := rapid.SampledFrom([]int{0, 1, 3, 4, 4, 5, 5, 5, 6, 7, 10, 50, 100}).Draw(t, "event_count")
		now := genNow().Draw(t, "now")
		history := make([]HistoryEvent, n)
		for i := range history {
			history[i] = HistoryEvent{
				At:             drawEventTime(t, now, history[:i]),
				AmountINR:      rapid.Int64Range(1, largest).Draw(t, "event_amount"),
				BeneficiaryRef: drawEventRef(t, "r***@okaxis"),
			}
		}
		if n > 0 {
			history[rapid.IntRange(0, n-1).Draw(t, "largest_index")].AmountINR = largest
		}
		return deviationCase{amount: amount, largest: largest, history: history}
	})
}

// deviationEntries returns the amount_deviation Contributions of an Explanation.
func deviationEntries(explanation []Contribution) []refEntry {
	var out []refEntry
	for _, c := range explanation {
		if c.Signal == "amount_deviation" {
			out = append(out, refEntry{Signal: c.Signal, Points: c.Points})
		}
	}
	return out
}

// Feature: risk-intelligence, Property 7: Amount deviation is exact for every int64
//
// For any payment amount and snapshot amounts from 1 to 9223372036854775807, the
// amount_deviation Contribution equals the answer of a model that multiplies with
// math/big: none for fewer than 5 events or an amount under 2000, otherwise 10
// points from 3 times and 15 points from 10 times the largest snapshot amount.
//
// **Validates: Requirements 9.11, 11.1, 11.2**
func TestAmountDeviationIsExactForEveryInt64(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := genDeviationCase().Draw(t, "case")
		in := Input{
			Signals:        genSignals().Draw(t, "signals"),
			Now:            genNow().Draw(t, "now"),
			History:        c.history,
			BeneficiaryRef: genPaymentRef().Draw(t, "ref"),
		}
		in.Signals.AmountINR = c.amount

		a, err := Assess(in)
		if err != nil {
			t.Fatalf("Assess(%+v) returned an error for valid signals: %v", in, err)
		}

		want := refDeviationPoints(c.amount, c.history)
		got := deviationEntries(a.Explanation)
		if problem := refReasonProblem(contributionsFor(a, "amount_deviation"), false); problem != "" {
			t.Fatal(problem)
		}
		switch {
		case want == 0 && len(got) != 0:
			t.Fatalf("amount %d, %d events, largest %d: got %v, want no amount_deviation Contribution",
				c.amount, len(c.history), c.largest, got)
		case want > 0 && (len(got) != 1 || got[0].Points != want):
			t.Fatalf("amount %d, %d events, largest %d: got %v, want %d points",
				c.amount, len(c.history), c.largest, got, want)
		}
	})
}

// TestUnitAmountDeviationExamples checks the worked example of Requirement 11
// criterion 1 and the two floors of criterion 2.
func TestUnitAmountDeviationExamples(t *testing.T) {
	// A snapshot of n events whose largest amount is largest and whose others
	// are half of it, or 1 when the largest is 1.
	snapshot := func(n int, largest int64) []HistoryEvent {
		filler := largest / 2
		if largest == 1 {
			filler = 1
		}
		out := make([]HistoryEvent, n)
		for i := range out {
			out[i] = HistoryEvent{At: 1_700_000_000, AmountINR: filler}
		}
		if n > 0 {
			out[n-1].AmountINR = largest
		}
		return out
	}
	tests := []struct {
		name    string
		amount  int64
		history []HistoryEvent
		want    int
		reason  string
	}{
		{"2999 is below 3 times 1000", 2999, snapshot(5, 1000), 0, ""},
		{"3000 is 3 times 1000", 3000, snapshot(5, 1000), 10, "This payment is much larger than your recent ones."},
		{"9999 is below 10 times 1000", 9999, snapshot(5, 1000), 10, "This payment is much larger than your recent ones."},
		{"10000 is 10 times 1000", 10000, snapshot(5, 1000), 15, "This payment is far larger than your recent ones."},
		{"4 events are too few", 10000, snapshot(4, 1000), 0, ""},
		{"an empty snapshot earns nothing", 10000, nil, 0, ""},
		{"1999 is below the amount floor", 1999, snapshot(5, 1), 0, ""},
		{"2000 meets the amount floor", 2000, snapshot(5, 1), 15, "This payment is far larger than your recent ones."},
		{"a largest amount of the int64 limit gives no overflow", math.MaxInt64, snapshot(5, math.MaxInt64), 0, ""},
		{"3 times a large amount is not reached by the int64 limit", math.MaxInt64, snapshot(5, math.MaxInt64/3+1), 0, ""},
		{"3 times a large amount is reached", math.MaxInt64, snapshot(5, math.MaxInt64/3), 10, "This payment is much larger than your recent ones."},
		{"10 times a large amount is reached", math.MaxInt64, snapshot(5, math.MaxInt64/10), 15, "This payment is far larger than your recent ones."},
		// Events with no amount above 0 cannot be compared with, so they earn
		// nothing. The service never records one, so no requirement covers it.
		{"events with no amount above 0 earn nothing", 10000, make([]HistoryEvent, 5), 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := Assess(Input{
				Signals: Signals{AmountINR: tt.amount, BeneficiaryAgeHrs: 720},
				Now:     1_700_000_100,
				History: tt.history,
			})
			if err != nil {
				t.Fatalf("Assess() error = %v, want nil", err)
			}
			var got []Contribution
			for _, c := range a.Explanation {
				if c.Signal == "amount_deviation" {
					got = append(got, c)
				}
			}
			if tt.want == 0 {
				if len(got) != 0 {
					t.Errorf("got %+v, want no amount_deviation Contribution", got)
				}
				return
			}
			want := []Contribution{{Signal: "amount_deviation", Points: tt.want, Reason: tt.reason}}
			if len(got) != 1 || got[0] != want[0] {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}
