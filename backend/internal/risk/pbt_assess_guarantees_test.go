package risk

import (
	"math"
	"testing"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
	"pgregory.net/rapid"
)

// The Signals of the three Presets, written from the Preset_Bar of the front end
// (dual-screen-simulator Requirement 2 criteria 2 to 4).
var (
	safePresetSignals     = Signals{AmountINR: 450, BeneficiaryAgeHrs: 720}
	mediumPresetSignals   = Signals{AmountINR: 18000, BeneficiaryAgeHrs: 6}
	highRiskPresetSignals = Signals{AmountINR: 85000, BeneficiaryAgeHrs: 1, IsLateNight: true, ActiveUnknownCall: true}
)

// The Signals of the three Fraud_Scenarios (Requirement 7 criteria 2 to 4).
var (
	digitalArrestSignals  = Signals{AmountINR: 60000, BeneficiaryAgeHrs: 2, IsLateNight: true, ActiveUnknownCall: true}
	courierCustomsSignals = Signals{AmountINR: 12500, BeneficiaryAgeHrs: 5, ActiveUnknownCall: true, RapidRetryCount: 1}
	kycExpirySignals      = Signals{AmountINR: 4999, BeneficiaryAgeHrs: 3, RapidRetryCount: 2}
)

// genLockedSignals draws valid Signals whose baseline is 75 or more. The first
// family is the precondition of behavioral-risk-engine Requirement 4 criterion 1
// (baseline 80 to 100). The other three reach a baseline of exactly 75 or 80 by
// other routes, so that the edge of the lock threshold is hit.
func genLockedSignals() *rapid.Generator[Signals] {
	return rapid.Custom(func(t *rapid.T) Signals {
		switch rapid.IntRange(0, 3).Draw(t, "family") {
		case 0:
			return genHeavySignals().Draw(t, "heavy")
		case 1:
			// 20 for the amount, 25 for the age, 10 late night, 20 call: 75.
			return Signals{
				AmountINR:         rapid.Int64Range(10000, 24999).Draw(t, "amount_inr"),
				BeneficiaryAgeHrs: rapid.Int64Range(0, 23).Draw(t, "beneficiary_age_hours"),
				IsLateNight:       true,
				ActiveUnknownCall: true,
				RapidRetryCount:   rapid.IntRange(0, 1).Draw(t, "rapid_retry_count"),
			}
		case 2:
			// 35 for the amount, 10 for the age, 10 late night, 20 call: 75.
			return Signals{
				AmountINR:         rapid.SampledFrom([]int64{25000, 85000, math.MaxInt64}).Draw(t, "amount_inr"),
				BeneficiaryAgeHrs: rapid.Int64Range(24, 71).Draw(t, "beneficiary_age_hours"),
				IsLateNight:       true,
				ActiveUnknownCall: true,
				RapidRetryCount:   rapid.IntRange(0, 1).Draw(t, "rapid_retry_count"),
			}
		default:
			// 35 for the amount, 25 for the age, 10 late night, 10 retries: 80,
			// with no unknown call.
			return Signals{
				AmountINR:         rapid.SampledFrom([]int64{25000, 85000, math.MaxInt64}).Draw(t, "amount_inr"),
				BeneficiaryAgeHrs: rapid.Int64Range(0, 23).Draw(t, "beneficiary_age_hours"),
				IsLateNight:       true,
				RapidRetryCount:   rapid.IntRange(3, 9).Draw(t, "rapid_retry_count"),
			}
		}
	})
}

// genZeroBaselineSignals draws valid Signals that earn no baseline point: an
// amount below 2000, a beneficiary age of 72 hours or more, neither switch on
// and at most 1 retry.
func genZeroBaselineSignals() *rapid.Generator[Signals] {
	return rapid.Custom(func(t *rapid.T) Signals {
		return Signals{
			AmountINR: rapid.OneOf(
				rapid.SampledFrom([]int64{1, 450, 1999}),
				rapid.Int64Range(1, 1999),
			).Draw(t, "amount_inr"),
			BeneficiaryAgeHrs: rapid.OneOf(
				rapid.SampledFrom([]int64{72, 73, 720, math.MaxInt64}),
				rapid.Int64Range(72, math.MaxInt64),
			).Draw(t, "beneficiary_age_hours"),
			RapidRetryCount: rapid.IntRange(0, 1).Draw(t, "rapid_retry_count"),
		}
	})
}

// Feature: risk-intelligence, Property 5: Lock, pass and preset guarantees
//
// A baseline of 75 or more gives a Risk_Score of at least 75 and LOCKED_15M for
// every History_Snapshot and every evaluation time. The inputs are the lock
// preconditions of behavioral-risk-engine, the other ways to reach 75 and 80,
// and any generated input whose baseline happens to be 75 or more.
//
// **Validates: Requirements 9.6, 27.4**
func TestBaselineOf75OrMoreIsAlwaysLocked(t *testing.T) {
	var checked int
	rapid.Check(t, func(t *rapid.T) {
		var in Input
		if rapid.Bool().Draw(t, "any_input") {
			in = genInput().Draw(t, "input")
		} else {
			in = genInputFor(genLockedSignals().Draw(t, "signals")).Draw(t, "input")
		}
		a, err := Assess(in)
		if err != nil {
			t.Fatalf("Assess(%+v) returned an error for valid signals: %v", in, err)
		}
		if a.Baseline < 75 {
			return
		}
		checked++
		if a.Score < 75 || a.State != store.StateLocked15M {
			t.Fatalf("baseline %d with %d events gives score %d and state %q, want at least 75 and %q",
				a.Baseline, len(in.History), a.Score, a.State, store.StateLocked15M)
		}
	})
	if checked == 0 {
		t.Fatal("no generated input had a baseline of 75 or more, so the check passed with nothing to check")
	}
}

// Feature: risk-intelligence, Property 5: Lock, pass and preset guarantees
//
// A baseline of 0 gives a Risk_Score of at most 35 and INSTANT_PASS for every
// History_Snapshot of 0 to 100 events and every evaluation time. The 35 is the
// Behaviour_Cap of 25 and the Care_Cap of 10 together.
//
// **Validates: Requirements 9.8, 27.4, 27.5**
func TestBaselineOfZeroIsAlwaysInstantPass(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		in := genInputFor(genZeroBaselineSignals().Draw(t, "signals")).Draw(t, "input")
		a, err := Assess(in)
		if err != nil {
			t.Fatalf("Assess(%+v) returned an error for valid signals: %v", in, err)
		}
		if a.Baseline != 0 {
			t.Fatalf("generator drew signals with baseline %d, want 0: %+v", a.Baseline, in.Signals)
		}
		if a.Score > 35 || a.State != store.StateInstantPass {
			t.Fatalf("baseline 0 with %d events gives score %d and state %q, want at most 35 and %q",
				len(in.History), a.Score, a.State, store.StateInstantPass)
		}
	})
}

// Feature: risk-intelligence, Property 5: Lock, pass and preset guarantees
//
// A baseline below 40 with no behavioural points and no care points gives a
// Risk_Score below 40 and INSTANT_PASS, which keeps the pass guarantee for the
// final score.
//
// **Validates: Requirements 9.7**
func TestLowBaselineWithNoExtraPointsStaysInstantPass(t *testing.T) {
	var checked int
	rapid.Check(t, func(t *rapid.T) {
		in := genInput().Draw(t, "input")
		a, err := Assess(in)
		if err != nil {
			t.Fatalf("Assess(%+v) returned an error for valid signals: %v", in, err)
		}
		if a.Baseline >= 40 || a.Behaviour != 0 {
			return
		}
		checked++
		if a.Score >= 40 || a.State != store.StateInstantPass {
			t.Fatalf("baseline %d and no extra points give score %d and state %q, want below 40 and %q",
				a.Baseline, a.Score, a.State, store.StateInstantPass)
		}
	})
	if checked == 0 {
		t.Fatal("no generated input had a baseline below 40 and no behavioural points, so the check passed with nothing to check")
	}
}

// Feature: risk-intelligence, Property 5: Lock, pass and preset guarantees
//
// The Signals of each of the three Presets give the documented state for every
// History_Snapshot, evaluation time and Beneficiary_Ref: INSTANT_PASS with a score
// of 0 to 25 for the Safe Preset, REALITY_CHECK with 45 to 70 for the Medium
// Preset, and LOCKED_15M with 90 to 100 for the High_Risk Preset. With an empty
// snapshot the score is the baseline.
//
// **Validates: Requirements 8.9, 9.9, 27.5**
func TestPresetsKeepTheirStatesForEveryHistory(t *testing.T) {
	presets := []struct {
		name     string
		signals  Signals
		baseline int
		state    store.VaultState
		lo, hi   int
	}{
		{"safe", safePresetSignals, 0, store.StateInstantPass, 0, 25},
		{"medium", mediumPresetSignals, 45, store.StateRealityCheck, 45, 70},
		{"high risk", highRiskPresetSignals, 90, store.StateLocked15M, 90, 100},
	}
	for _, p := range presets {
		t.Run(p.name, func(t *testing.T) {
			rapid.Check(t, func(t *rapid.T) {
				in := genInputFor(p.signals).Draw(t, "input")
				a, err := Assess(in)
				if err != nil {
					t.Fatalf("Assess(%+v) returned an error for valid signals: %v", in, err)
				}
				if a.Baseline != p.baseline {
					t.Fatalf("baseline = %d, want %d", a.Baseline, p.baseline)
				}
				if a.State != p.state || a.Score < p.lo || a.Score > p.hi {
					t.Fatalf("%d events give score %d and state %q, want %d to %d and %q",
						len(in.History), a.Score, a.State, p.lo, p.hi, p.state)
				}
				if len(in.History) == 0 && a.Score != p.baseline {
					t.Fatalf("an empty snapshot gives score %d, want the baseline %d", a.Score, p.baseline)
				}
			})
		})
	}
}

// Feature: risk-intelligence, Property 5: Lock, pass and preset guarantees
//
// The Signals of the three Fraud_Scenarios earn 90, 65 and 40 baseline points.
// The score is never below that for any History_Snapshot, and equals it for an
// empty snapshot. The Digital_Arrest signals are always LOCKED_15M. The
// Courier_Customs and KYC_Expiry signals are REALITY_CHECK while the behavioural
// points are 0, and KYC_Expiry is never INSTANT_PASS.
//
// **Validates: Requirements 7.2, 7.3, 7.4, 7.9**
func TestFraudScenarioSignalsKeepTheirBaselineForEveryHistory(t *testing.T) {
	stories := []struct {
		name     string
		signals  Signals
		baseline int
	}{
		{"digital arrest", digitalArrestSignals, 90},
		{"courier and customs", courierCustomsSignals, 65},
		{"kyc expiry", kycExpirySignals, 40},
	}
	for _, s := range stories {
		t.Run(s.name, func(t *testing.T) {
			rapid.Check(t, func(t *rapid.T) {
				in := genInputFor(s.signals).Draw(t, "input")
				a, err := Assess(in)
				if err != nil {
					t.Fatalf("Assess(%+v) returned an error for valid signals: %v", in, err)
				}
				if a.Baseline != s.baseline {
					t.Fatalf("baseline = %d, want %d", a.Baseline, s.baseline)
				}
				if a.Score < s.baseline {
					t.Fatalf("score %d is below the baseline %d", a.Score, s.baseline)
				}
				if len(in.History) == 0 && a.Score != s.baseline {
					t.Fatalf("an empty snapshot gives score %d, want the baseline %d", a.Score, s.baseline)
				}
				if s.baseline == 90 && a.State != store.StateLocked15M {
					t.Fatalf("state = %q, want %q", a.State, store.StateLocked15M)
				}
				if s.baseline < 75 {
					if a.State == store.StateInstantPass {
						t.Fatalf("state = %q with a baseline of %d", a.State, s.baseline)
					}
					if a.Behaviour == 0 && a.State != store.StateRealityCheck {
						t.Fatalf("state = %q with no behavioural points, want %q", a.State, store.StateRealityCheck)
					}
				}
			})
		})
	}
}
