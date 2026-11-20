package risk

import (
	"flag"
	"math"
	"strconv"
	"testing"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// passMinChecks is the minimum number of generated inputs per property run
// (Requirement 5.2). rapid defaults to 100, so the flag is raised here instead
// of relying on -rapid.checks being passed on the command line.
const passMinChecks = 1000

// init raises rapid's check count to passMinChecks when the rapid.checks flag
// is registered and set lower. An explicit -rapid.checks on the command line
// is applied later by flag.Parse and still takes effect.
func init() {
	f := flag.Lookup("rapid.checks")
	if f == nil {
		return
	}
	n, err := strconv.Atoi(f.Value.String())
	if err == nil && n >= passMinChecks {
		return
	}
	if err := flag.Set("rapid.checks", strconv.Itoa(passMinChecks)); err != nil {
		panic("risk: raise rapid.checks: " + err.Error())
	}
}

// passAmountGen draws valid amounts below 2000 rupees, mixing the two ends of
// that range (1 is the smallest valid amount, 1999 the last amount in the
// zero-point tier) with the full range.
func passAmountGen() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{1, 1999}),
		rapid.Int64Range(1, 1999),
	)
}

// passAgeGen draws beneficiary ages of 72 hours or more (a Known_Contact),
// mixing the 72 boundary and the largest possible age with ordinary and wide
// ranges.
func passAgeGen() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{72, 73, math.MaxInt64}),
		rapid.Int64Range(72, 1000),
		rapid.Int64Range(72, math.MaxInt64),
	)
}

// passRetryGen draws the retry counts that earn no retry points: 0 or 1.
func passRetryGen() *rapid.Generator[int] {
	return rapid.IntRange(0, 1)
}

// passSignalsGen draws Signals that satisfy every precondition of the pass
// guarantee in Requirement 4.2: a small amount, a Known_Contact, daytime, no
// unknown call and at most one retry.
func passSignalsGen() *rapid.Generator[Signals] {
	return rapid.Custom(func(t *rapid.T) Signals {
		return Signals{
			AmountINR:         passAmountGen().Draw(t, "amount_inr"),
			BeneficiaryAgeHrs: passAgeGen().Draw(t, "beneficiary_age_hours"),
			IsLateNight:       false,
			ActiveUnknownCall: false,
			RapidRetryCount:   passRetryGen().Draw(t, "rapid_retry_count"),
		}
	})
}

// **Validates: Requirements 4.2, 5.2**
//
// Feature: behavioral-risk-engine, Property 6: Pass guarantee
func TestPassGuaranteeHolds(t *testing.T) {
	// The bound and the state are written as the literal 40 and the store
	// constant, so a wrong RealityCheckThreshold constant is caught too.
	rapid.Check(t, func(t *rapid.T) {
		s := passSignalsGen().Draw(t, "signals")

		res, err := Evaluate(s)
		if err != nil {
			t.Fatalf("Evaluate(%+v) returned an error for valid signals: %v", s, err)
		}
		if res.Score >= 40 {
			t.Fatalf("Evaluate(%+v).Score = %d, want below 40", s, res.Score)
		}
		if res.State != store.StateInstantPass {
			t.Fatalf("Evaluate(%+v).State = %q, want %q", s, res.State, store.StateInstantPass)
		}
	})
}
