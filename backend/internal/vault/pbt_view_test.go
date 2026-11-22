package vault

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/redact"
	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// viewIsSafe fails the test unless the JSON form of v holds no signing key and
// no OTP, PIN or password field.
func viewIsSafe(rt *rapid.T, v View) {
	rt.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		rt.Fatalf("marshal View: %v", err)
	}
	text := string(body)
	if strings.Contains(text, harnessSecret) {
		rt.Fatalf("View holds the signing key: %s", text)
	}
	if redacted := redact.Text(text); redacted != text {
		rt.Fatalf("View holds an OTP, PIN or password field: %s", text)
	}
}

// Feature: cooling-off-vault, Property 17: Read view is safe and complete
//
// **Validates: Requirements 7.1, 7.2, 7.3, 8.9, 5.9**
func TestReadViewIsSafeAndComplete(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		state := stateGen().Draw(rt, "state")
		ids := identifiersGen().Draw(rt, "identifiers")
		// A vault stays visible for an hour after the moment it must exist, and
		// an EXPIRED vault needs the lock to run out first, so keep the offset
		// small.
		offset := rapid.Int64Range(0, 600).Draw(rt, "offset_s")

		e := newEnv(false)
		created := e.vaultInWith(rt, state, ids)
		e.clock.AdvanceSeconds(offset)

		got, err := e.svc.Get(context.Background(), created.ID)
		if err != nil {
			rt.Fatalf("Get = %v, want nil", err)
		}
		rec := e.record(rt, created.ID)
		now := e.clock.Now().Unix()

		if got.ID != created.ID || got.State != rec.State || got.Settled != rec.Settled || got.RiskScore != rec.RiskScore {
			rt.Fatalf("View = %+v does not match the record %+v", got, rec)
		}
		if got.State != state {
			rt.Fatalf("View state %s, want %s", got.State, state)
		}
		if got.Reason == "" {
			rt.Fatalf("View has no reason in state %s", got.State)
		}
		wantMeta := Metadata{
			PayerAccountMasked: ids.PayerAccountMasked,
			BeneficiaryMasked:  ids.BeneficiaryMasked,
			UPIMasked:          ids.UPIMasked,
			PhoneMasked:        ids.PhoneMasked,
			AmountINR:          rec.AmountINR,
		}
		if got.Metadata != wantMeta {
			rt.Fatalf("Metadata = %+v, want %+v", got.Metadata, wantMeta)
		}

		// The token appears only while locked, and the evidence only once vetoed.
		if got.State == store.StateLocked15M {
			if want := (testIssuer{}).Issue(got.ID, rec.TokenExpiry); got.GuardianToken != want {
				rt.Fatalf("GuardianToken = %q, want %q", got.GuardianToken, want)
			}
			if want := rec.LockDeadline - now; got.RemainingSeconds != want || want <= 0 {
				rt.Fatalf("RemainingSeconds = %d, want %d", got.RemainingSeconds, want)
			}
		} else if got.GuardianToken != "" || got.RemainingSeconds != 0 {
			rt.Fatalf("a %s vault has token %q and %d seconds remaining", got.State, got.GuardianToken, got.RemainingSeconds)
		}
		if got.State == store.StateVetoed {
			if got.Evidence == "" || got.Evidence != rec.EvidenceSummary {
				rt.Fatalf("Evidence = %q, want the stored summary %q", got.Evidence, rec.EvidenceSummary)
			}
		} else if got.Evidence != "" {
			rt.Fatalf("a %s vault carries evidence %q", got.State, got.Evidence)
		}

		viewIsSafe(rt, got)
		for _, published := range e.obs.all() {
			viewIsSafe(rt, published)
		}
	})
}
