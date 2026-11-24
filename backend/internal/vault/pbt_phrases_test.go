package vault

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/redact"
	"github.com/RahulKumar1119/rukpay/backend/internal/risk"
	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// signalIDGen draws a Signal_Id: every id the engine writes, a pattern id, and
// ids it never writes. It leans toward the behavioural ones, so that lists with
// two or more phrases are common.
func signalIDGen() *rapid.Generator[string] {
	behavioural := []string{
		risk.SignalVelocity, risk.SignalDailyFrequency, risk.SignalRepeatAttempts,
		risk.SignalAmountDeviation, risk.SignalPatternPrefix + "test_then_large",
	}
	others := []string{
		risk.SignalAmount, risk.SignalBeneficiaryAge, risk.SignalLateNight, risk.SignalUnknownCall,
		risk.SignalRapidRetries, risk.SignalBehaviourCap, risk.SignalScoreCap, risk.SignalBeneficiaryCare,
		"", "unknown", "Velocity", "pattern", "velocity_extra",
	}
	return rapid.OneOf(
		rapid.SampledFrom(behavioural), rapid.SampledFrom(behavioural), rapid.SampledFrom(behavioural),
		rapid.SampledFrom(others),
		rapid.Map(rapid.StringMatching(`^[a-z_]{1,12}$`), func(s string) string { return risk.SignalPatternPrefix + s }),
		rapid.StringN(0, 12, -1),
	)
}

// explanationGen draws an Explanation of 0 to 10 lines with any Signal_Id, any
// points from below 0 to above 0, and any reason text. Nothing about it needs to
// be one that the engine would write: the rule under test reads the Signal_Id
// and whether the points are above 0, and nothing else.
func explanationGen() *rapid.Generator[[]risk.Contribution] {
	return rapid.SliceOfN(rapid.Custom(func(t *rapid.T) risk.Contribution {
		return risk.Contribution{
			Signal: signalIDGen().Draw(t, "signal"),
			Points: rapid.OneOf(rapid.SampledFrom([]int{-30, -5, -1, 0, 1, 5, 10, 15, 35}), rapid.IntRange(-40, 40)).Draw(t, "points"),
			Reason: rapid.String().Draw(t, "reason"),
		}
	}), 0, 10)
}

// joinList joins items the way Requirement 3.8 says, written with a loop and not
// with the code of messages.go: one item alone, "a and b" for two, and
// "a, b and c" for three or more.
func joinList(items []string) string {
	var b strings.Builder
	for i, item := range items {
		switch {
		case i == 0:
		case i == len(items)-1:
			b.WriteString(" and ")
		default:
			b.WriteString(", ")
		}
		b.WriteString(item)
	}
	return b.String()
}

// modelReasons lists the reason phrases of the signals of s that are true, in the
// order amount, recipient age, late hour, call, retries, from the thresholds of
// the risk engine's first tier and not from the code of messages.go.
func modelReasons(s risk.Signals) []string {
	var out []string
	if s.AmountINR >= 10000 {
		out = append(out, msgReasonLargeAmount)
	}
	if s.BeneficiaryAgeHrs < 24 {
		out = append(out, msgReasonNewRecipient)
	} else if s.BeneficiaryAgeHrs < 72 {
		out = append(out, msgReasonFairlyNew)
	}
	if s.IsLateNight {
		out = append(out, msgReasonLateHour)
	}
	if s.ActiveUnknownCall {
		out = append(out, msgReasonUnknownCall)
	}
	if s.RapidRetryCount >= 2 {
		out = append(out, msgReasonQuickRetries)
	}
	return out
}

// modelPhrases lists the phrases that Requirement 3.8 adds for ex: one for each
// line with more than 0 points whose Signal_Id is velocity, daily_frequency,
// repeat_attempts, amount_deviation or starts with pattern_, in the order of ex.
func modelPhrases(ex []risk.Contribution) []string {
	var out []string
	for _, c := range ex {
		if c.Points <= 0 {
			continue
		}
		if p, ok := phraseOfSignal(c.Signal); ok {
			out = append(out, p)
		}
	}
	return out
}

// modelReason is the vault reason of Requirement 3.8, 3.9 and 3.11 written out
// again. It uses reasonFor only for the cases where the requirement says the old
// sentence is kept.
func modelReason(state store.VaultState, s risk.Signals, ex []risk.Contribution) string {
	added := modelPhrases(ex)
	if (state != store.StateRealityCheck && state != store.StateLocked15M) || len(added) == 0 {
		return reasonFor(state, s)
	}
	joined := joinList(append(modelReasons(s), added...))
	if state == store.StateRealityCheck {
		return redact.Text(fmt.Sprintf(msgRealityCheckFmt, joined))
	}
	return redact.Text(fmt.Sprintf(msgLockedFmt, joined))
}

// Feature: risk-intelligence, Property 12: The vault reason keeps its old form and gains calm phrases
//
// For any state, any signals and any Explanation, reasonForExplained gives the
// reason of Requirement 3.8. With no behavioural line of more than 0 points it is
// byte for byte what reasonFor gives, and for every state other than
// REALITY_CHECK and LOCKED_15M it is always what reasonFor gives. Otherwise it is
// the old sentence with one phrase added for each such line, in Explanation order,
// joined as one, "a and b", or "a, b and c". Lines for the baseline, for a cap, for
// 0 or fewer points and for ids with no phrase add nothing. The same text comes
// out when the points and the reasons of the lines change but their Signal_Ids and
// the sign of their points do not, so the choice rests on the Signal_Id alone.
// The text is calm, unchanged by redact.Text, and a locked reason states the
// 15-minute pause.
//
// **Validates: Requirements 3.8, 3.9, 3.10, 3.11**
func TestReasonKeepsItsOldFormAndGainsCalmPhrases(t *testing.T) {
	phrased := 0
	rapid.Check(t, func(rt *rapid.T) {
		state := stateGen().Draw(rt, "state")
		s := signalsGen().Draw(rt, "signals")
		ex := explanationGen().Draw(rt, "explanation")

		got := reasonForExplained(state, s, ex)
		want := modelReason(state, s, ex)
		if got != want {
			rt.Fatalf("reasonForExplained(%s, %+v, %+v) =\n  %q\nwant\n  %q", state, s, ex, got, want)
		}

		added := modelPhrases(ex)
		if len(added) == 0 || (state != store.StateRealityCheck && state != store.StateLocked15M) {
			if got != reasonFor(state, s) {
				rt.Fatalf("with no phrase to add the reason is %q, want the old %q", got, reasonFor(state, s))
			}
		} else {
			phrased++
			for _, p := range added {
				if !strings.Contains(got, p) {
					rt.Fatalf("the reason %q does not name the phrase %q", got, p)
				}
			}
			// The phrases come last and in order.
			tail := joinList(append(modelReasons(s), added...))
			if !strings.Contains(got, tail) {
				rt.Fatalf("the reason %q does not hold the joined list %q", got, tail)
			}
		}

		// The choice rests on the Signal_Id and on points above 0, and on nothing
		// else: change every other part of every line, and the reason stays.
		twin := make([]risk.Contribution, len(ex))
		for i, c := range ex {
			twin[i] = risk.Contribution{Signal: c.Signal, Reason: "Another sentence entirely."}
			switch {
			case c.Points > 0:
				twin[i].Points = c.Points + 1 + i
			case c.Points == 0:
				twin[i].Points = 0
			default:
				twin[i].Points = c.Points - 1 - i
			}
		}
		if again := reasonForExplained(state, s, twin); again != got {
			rt.Fatalf("changing points and reasons changed the reason from %q to %q", got, again)
		}

		checkCalm(rt, "reason", got)
		if redact.Text(got) != got {
			rt.Fatalf("reason %q is changed by redact.Text", got)
		}
		if state == store.StateLocked15M && !strings.Contains(got, "15-minute") {
			rt.Fatalf("a locked reason %q does not state the 15-minute pause", got)
		}
	})
	if phrased == 0 {
		t.Fatal("no generated case gained a phrase, so the property did not check the new form")
	}
}

// Feature: risk-intelligence, Property 12: The vault reason keeps its old form and gains calm phrases
//
// What Evaluate stores is what reasonForExplained gives for the Explanation it
// stores, so the reason a vault is created with, for every state, is the one of
// Requirement 3.8 and is not changed afterwards: the first View and a later read
// of the stored record show the same reason, and a payment that passes at once has
// the sentence it always had.
//
// **Validates: Requirements 3.8, 3.9, 3.11**
func TestCreatedVaultReasonIsTheModelReason(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		e := newHistEnv(false)
		for i, n := 0, rapid.IntRange(0, 12).Draw(rt, "seed_count"); i < n; i++ {
			at := histEpoch() - rapid.Int64Range(0, 700).Draw(rt, "seed_age")
			e.seed(rt, store.HistoryEvent{
				ID: seedID("s", i), At: at,
				AmountINR:      rapid.SampledFrom([]int64{100, 450, 1000, 5000}).Draw(rt, "seed_amount"),
				BeneficiaryRef: rapid.SampledFrom(poolRefs()).Draw(rt, "seed_ref"),
				ExpiresAt:      store.HistoryExpiry(at),
			})
		}
		req := pooledIdentifiersGen().Draw(rt, "identifiers")
		req.Signals = explainSignalsGen().Draw(rt, "signals")

		res := e.evaluate(req)
		if res.err != nil {
			rt.Fatalf("Evaluate = %v, want nil", res.err)
		}
		rec := e.record(rt, res.view.ID)
		want := modelReason(rec.State, req.Signals, rec.Explanation)
		if res.view.Reason != want || rec.Reason != want {
			rt.Fatalf("reason: view %q, record %q, want %q (state %s)", res.view.Reason, rec.Reason, want, rec.State)
		}
		if rec.State == store.StateInstantPass && rec.Reason != msgInstantPass {
			rt.Fatalf("a payment that passes at once has the reason %q, want %q", rec.Reason, msgInstantPass)
		}
		if again, err := e.svc.Get(context.Background(), res.view.ID); err != nil || again.Reason != want {
			rt.Fatalf("a later read gave the reason %q (%v), want %q", again.Reason, err, want)
		}
	})
}
