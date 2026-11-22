package vault

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/risk"
	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// Feature: cooling-off-vault, Property 1: Creation matches the risk engine
//
// **Validates: Requirements 1.1, 1.2, 1.3, 1.4, 1.8**
func TestCreationMatchesTheRiskEngine(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		sig := signalsGen().Draw(rt, "signals")
		offset := rapid.Int64Range(0, 3*86400).Draw(rt, "clock_offset_s")

		e := newEnv(false)
		e.clock.AdvanceSeconds(offset)
		created := harnessEpoch().Unix() + offset

		want, err := risk.Evaluate(sig)
		if err != nil {
			rt.Fatalf("risk.Evaluate(%+v) = %v, want nil", sig, err)
		}
		v := e.create(rt, sig)
		rec := e.record(rt, v.ID)

		if v.State != want.State || v.RiskScore != want.Score {
			rt.Fatalf("View = state %s score %d, want %s %d", v.State, v.RiskScore, want.State, want.Score)
		}
		if rec.State != want.State || rec.RiskScore != want.Score {
			rt.Fatalf("record = state %s score %d, want %s %d", rec.State, rec.RiskScore, want.State, want.Score)
		}
		if rec.CreatedAt.Unix() != created || rec.AmountINR != sig.AmountINR {
			rt.Fatalf("record = created %d amount %d, want %d %d", rec.CreatedAt.Unix(), rec.AmountINR, created, sig.AmountINR)
		}
		if wantSettled := want.State == store.StateInstantPass; rec.Settled != wantSettled || v.Settled != wantSettled {
			rt.Fatalf("Settled = record %v view %v, want %v for state %s", rec.Settled, v.Settled, wantSettled, want.State)
		}
		if rec.Reason == "" || v.Reason != rec.Reason {
			rt.Fatalf("Reason = view %q record %q, want one non-empty message", v.Reason, rec.Reason)
		}

		if want.State == store.StateLocked15M {
			deadline := created + LockDuration
			if rec.LockDeadline != deadline || rec.TokenExpiry != deadline {
				rt.Fatalf("LockDeadline %d TokenExpiry %d, want both %d", rec.LockDeadline, rec.TokenExpiry, deadline)
			}
			if rec.ExpiresAt.Unix() < deadline+3600 {
				rt.Fatalf("ExpiresAt %d is earlier than the deadline plus 3600 (%d)", rec.ExpiresAt.Unix(), deadline+3600)
			}
			if wantToken := (testIssuer{}).Issue(v.ID, deadline); v.GuardianToken != wantToken {
				rt.Fatalf("GuardianToken = %q, want %q", v.GuardianToken, wantToken)
			}
			if v.RemainingSeconds != LockDuration {
				rt.Fatalf("RemainingSeconds = %d, want %d", v.RemainingSeconds, LockDuration)
			}
			return
		}

		if rec.LockDeadline != 0 || rec.TokenExpiry != 0 {
			rt.Fatalf("an unlocked vault has LockDeadline %d TokenExpiry %d, want 0", rec.LockDeadline, rec.TokenExpiry)
		}
		if v.GuardianToken != "" {
			rt.Fatalf("an unlocked vault has guardian token %q", v.GuardianToken)
		}
		if rec.ExpiresAt.Unix() < created+3600 {
			rt.Fatalf("ExpiresAt %d is earlier than creation plus 3600 (%d)", rec.ExpiresAt.Unix(), created+3600)
		}
	})
}
