package guardian

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/redact"
	"github.com/RahulKumar1119/rukpay/backend/internal/store"
	"github.com/RahulKumar1119/rukpay/backend/internal/vault"
)

// unknownActionGen draws an action name that is neither APPROVE nor VETO.
func unknownActionGen() *rapid.Generator[string] {
	return rapid.OneOf(
		rapid.SampledFrom([]string{"", "approve", "veto", "Approve", "Veto", "DELETE", "APPROVE ", " VETO", "BLOCK"}),
		rapid.String(),
	).Filter(func(s string) bool { return s != ActionApprove && s != ActionVeto })
}

// Feature: cooling-off-vault, Property 14: Approval needs a confirmed second tap
//
// With a valid token on a locked vault before its deadline, APPROVE with
// confirmed true approves and settles it. APPROVE without confirmation answers
// CONFIRMATION_REQUIRED and changes nothing.
//
// **Validates: Requirements 6.1, 6.2**
func TestApprovalNeedsAConfirmedSecondTap(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		offset := rapid.Int64Range(0, 899).Draw(rt, "offset_s")
		confirmed := rapid.Bool().Draw(rt, "confirmed")

		e := newEnv(false)
		v := e.vaultIn(rt, store.StateLocked15M)
		e.clock.AdvanceSeconds(offset)
		before := e.record(rt, v.ID)
		_, writesBefore, _ := e.cs.snapshot()

		res, err := e.guardian.Act(context.Background(), v.ID, Action{
			Action:    ActionApprove,
			Token:     v.GuardianToken,
			Confirmed: confirmed,
		})
		if err != nil {
			rt.Fatalf("Act(APPROVE, confirmed %v) at +%ds = %v, want nil", confirmed, offset, err)
		}
		rec := e.record(rt, v.ID)

		if !confirmed {
			if res.Status != StatusConfirmationRequired || res.View.State != store.StateLocked15M {
				rt.Fatalf("unconfirmed APPROVE = %+v, want CONFIRMATION_REQUIRED on a LOCKED_15M vault", res)
			}
			if rec != before {
				rt.Fatalf("unconfirmed APPROVE changed the record: %+v -> %+v", before, rec)
			}
			if _, writes, _ := e.cs.snapshot(); writes != writesBefore {
				rt.Fatalf("unconfirmed APPROVE wrote to the store")
			}
			return
		}

		if res.Status != StatusOK || res.View.State != store.StateGuardianApproved || !res.View.Settled {
			rt.Fatalf("confirmed APPROVE = %+v, want OK with a settled GUARDIAN_APPROVED view", res)
		}
		if rec.State != store.StateGuardianApproved || !rec.Settled {
			rt.Fatalf("stored record after APPROVE = %s settled %v", rec.State, rec.Settled)
		}
	})
}

// Feature: cooling-off-vault, Property 14: Approval needs a confirmed second tap
//
// An action that is neither APPROVE nor VETO is refused with ErrInvalidAction
// after the token check, and nothing changes.
//
// **Validates: Requirements 6.5**
func TestUnknownActionIsRefused(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		name := unknownActionGen().Draw(rt, "action")
		confirmed := rapid.Bool().Draw(rt, "confirmed")

		e := newEnv(false)
		v := e.vaultIn(rt, store.StateLocked15M)
		before := e.record(rt, v.ID)
		_, writesBefore, _ := e.cs.snapshot()

		_, err := e.guardian.Act(context.Background(), v.ID, Action{Action: name, Token: v.GuardianToken, Confirmed: confirmed})
		if !errors.Is(err, ErrInvalidAction) {
			rt.Fatalf("Act(%q) = %v, want ErrInvalidAction", name, err)
		}
		if after := e.record(rt, v.ID); after != before {
			rt.Fatalf("Act(%q) changed the record", name)
		}
		if _, writes, _ := e.cs.snapshot(); writes != writesBefore {
			rt.Fatalf("Act(%q) wrote to the store", name)
		}
	})
}

// rawIdentifierPatterns match values that are not masked: a bare account
// number, a bare phone number and a UPI id with a readable name before the @.
func rawIdentifierPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`\b\d{9,18}\b`),
		regexp.MustCompile(`\b[6-9]\d{9}\b`),
		regexp.MustCompile(`\+91[- ]?\d{10}`),
		regexp.MustCompile(`[A-Za-z0-9._-]{2,}@[A-Za-z0-9.-]+`),
	}
}

// Feature: cooling-off-vault, Property 15: Veto cancels and produces masked evidence
//
// **Validates: Requirements 6.3, 6.4, 6.8**
func TestVetoCancelsAndProducesMaskedEvidence(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		ids := identifiersGen().Draw(rt, "identifiers")
		offset := rapid.Int64Range(0, 899).Draw(rt, "offset_s")

		e := newEnv(false)
		v := e.vaultInWith(rt, store.StateLocked15M, ids)
		e.clock.AdvanceSeconds(offset)

		res, err := e.guardian.Act(context.Background(), v.ID, Action{Action: ActionVeto, Token: v.GuardianToken})
		if err != nil {
			rt.Fatalf("Act(VETO) at +%ds = %v, want nil", offset, err)
		}
		if res.Status != StatusOK || res.View.State != store.StateVetoed || res.View.Settled {
			rt.Fatalf("VETO = %+v, want OK with an unsettled VETOED view", res)
		}

		evidence := res.View.Evidence
		for _, want := range []string{v.ID, "1930", "cybercrime.gov.in"} {
			if !strings.Contains(evidence, want) {
				rt.Fatalf("evidence %q does not contain %q", evidence, want)
			}
		}
		for _, masked := range []string{ids.PayerAccountMasked, ids.BeneficiaryMasked, ids.UPIMasked, ids.PhoneMasked} {
			if masked != "" && !strings.Contains(evidence, masked) {
				rt.Fatalf("evidence %q does not contain the masked identifier %q", evidence, masked)
			}
		}
		for _, pattern := range rawIdentifierPatterns() {
			if loc := pattern.FindString(evidence); loc != "" {
				rt.Fatalf("evidence holds %q, which looks like an unmasked identifier:\n%s", loc, evidence)
			}
		}
		if redact.Text(evidence) != evidence {
			rt.Fatalf("evidence holds an OTP, PIN or password field:\n%s", evidence)
		}

		// Every later read returns the same evidence, and the stored copy matches.
		for i := 0; i < 2; i++ {
			got, err := e.vault.Get(context.Background(), v.ID)
			if err != nil || got.State != store.StateVetoed || got.Evidence != evidence {
				rt.Fatalf("Get #%d = %+v, %v, want the same evidence", i, got, err)
			}
		}
		if rec := e.record(rt, v.ID); rec.EvidenceSummary != evidence {
			rt.Fatalf("stored evidence differs from the response")
		}
	})
}

// Feature: cooling-off-vault, Property 16: Guardian actions on non-locked or expired vaults are refused
//
// For any vault not in LOCKED_15M, an action with a valid token is a conflict
// that carries the current state, and the record is unchanged.
//
// **Validates: Requirements 6.6**
func TestActionsOnNonLockedVaultsAreRefused(t *testing.T) {
	actions := []Action{
		{Action: ActionApprove, Confirmed: true},
		{Action: ActionApprove, Confirmed: false},
		{Action: ActionVeto},
	}
	rapid.Check(t, func(rt *rapid.T) {
		state := rapid.SampledFrom(nonLockedStates()).Draw(rt, "state")
		action := rapid.SampledFrom(actions).Draw(rt, "action")

		e := newEnv(false)
		v := e.vaultIn(rt, state)
		action.Token = e.longToken(v.ID)
		before := e.record(rt, v.ID)
		_, writesBefore, _ := e.cs.snapshot()

		_, err := e.guardian.Act(context.Background(), v.ID, action)
		var conflict *vault.ConflictError
		if !errors.As(err, &conflict) || conflict.Current.State != state {
			rt.Fatalf("%+v on a %s vault = %v, want a conflict carrying that state", action, state, err)
		}
		if after := e.record(rt, v.ID); after != before {
			rt.Fatalf("%+v on a %s vault changed the record: %+v -> %+v", action, state, before, after)
		}
		if _, writes, _ := e.cs.snapshot(); writes != writesBefore {
			rt.Fatalf("%+v on a %s vault wrote to the store", action, state)
		}
	})
}

// Feature: cooling-off-vault, Property 16: Guardian actions on non-locked or expired vaults are refused
//
// An action sent at or after lock_deadline never approves the vault. With the
// honest token the token has run out, so the answer is ErrForbidden. With a
// token that is still valid the vault has expired, so the answer is a conflict
// on an EXPIRED vault.
//
// **Validates: Requirements 6.6, 12.10**
func TestActionsAtOrAfterTheDeadlineNeverApprove(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		offset := rapid.OneOf(
			rapid.SampledFrom([]int64{900, 901, 902}),
			rapid.Int64Range(900, 3000),
		).Draw(rt, "offset_s")
		action := rapid.SampledFrom([]Action{
			{Action: ActionApprove, Confirmed: true},
			{Action: ActionApprove, Confirmed: false},
			{Action: ActionVeto},
		}).Draw(rt, "action")
		honest := rapid.Bool().Draw(rt, "honest_token")

		e := newEnv(false)
		v := e.vaultIn(rt, store.StateLocked15M)
		action.Token = v.GuardianToken
		if !honest {
			action.Token = e.longToken(v.ID)
		}
		e.clock.AdvanceSeconds(offset)

		_, err := e.guardian.Act(context.Background(), v.ID, action)

		if honest {
			if err != ErrForbidden {
				rt.Fatalf("%+v with the honest token at +%ds = %v, want ErrForbidden", action, offset, err)
			}
		} else {
			var conflict *vault.ConflictError
			if !errors.As(err, &conflict) || conflict.Current.State != store.StateExpired {
				rt.Fatalf("%+v with a valid token at +%ds = %v, want a conflict on an EXPIRED vault", action, offset, err)
			}
		}

		// Whatever happened, the vault never ends approved or vetoed.
		got, err := e.vault.Get(context.Background(), v.ID)
		if err != nil || got.State != store.StateExpired {
			rt.Fatalf("Get after the deadline = %+v, %v, want an EXPIRED vault", got, err)
		}
	})
}
