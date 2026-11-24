package vault

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// Feature: cooling-off-vault, Property 8: Lazy expiry follows the clock
//
// A read before lock_deadline reports the remaining seconds. A read at or after
// it reports an unsettled EXPIRED vault. Repeating the read changes nothing.
//
// **Validates: Requirements 4.1, 4.2, 7.5**
func TestLazyExpiryFollowsTheClock(t *testing.T) {
	offsets := rapid.OneOf(
		rapid.SampledFrom([]int64{0, 1, 2, 898, 899, 900, 901, 902}),
		rapid.Int64Range(0, 3000),
	)
	rapid.Check(t, func(rt *rapid.T) {
		offset := offsets.Draw(rt, "offset_s")

		e := newEnv(false)
		v := e.create(rt, signalsLocked())
		e.clock.AdvanceSeconds(offset)
		ctx := context.Background()

		got, err := e.svc.Get(ctx, v.ID)
		if err != nil {
			rt.Fatalf("Get at +%ds = %v, want nil", offset, err)
		}

		if offset < LockDuration {
			if got.State != store.StateLocked15M || got.Settled {
				rt.Fatalf("Get at +%ds = state %s settled %v, want an unsettled LOCKED_15M", offset, got.State, got.Settled)
			}
			if want := LockDuration - offset; got.RemainingSeconds != want {
				rt.Fatalf("RemainingSeconds at +%ds = %d, want %d", offset, got.RemainingSeconds, want)
			}
			if got.GuardianToken == "" || got.ReconfirmNeeded {
				rt.Fatalf("a locked vault at +%ds: token %q reconfirm %v", offset, got.GuardianToken, got.ReconfirmNeeded)
			}
		} else {
			if got.State != store.StateExpired || got.Settled {
				rt.Fatalf("Get at +%ds = state %s settled %v, want an unsettled EXPIRED", offset, got.State, got.Settled)
			}
			if got.RemainingSeconds != 0 || got.GuardianToken != "" || !got.ReconfirmNeeded {
				rt.Fatalf("an expired vault at +%ds: remaining %d token %q reconfirm %v",
					offset, got.RemainingSeconds, got.GuardianToken, got.ReconfirmNeeded)
			}
		}

		// Reading again returns the same state, and an expiry is written once.
		writes := e.cs.writeCount()
		again, err := e.svc.Get(ctx, v.ID)
		if err != nil || !reflect.DeepEqual(again, got) {
			rt.Fatalf("second Get = %+v, %v, want %+v", again, err, got)
		}
		if e.cs.writeCount() != writes {
			rt.Fatalf("the second Get wrote to the store")
		}
	})
}

// Feature: cooling-off-vault, Property 8: Lazy expiry follows the clock
//
// An action sent to a locked vault at or after its deadline meets an EXPIRED
// vault, so it conflicts and the vault ends EXPIRED.
//
// **Validates: Requirements 4.2, 12.10**
func TestActionsAtOrAfterTheDeadlineMeetAnExpiredVault(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		offset := rapid.OneOf(
			rapid.SampledFrom([]int64{900, 901}),
			rapid.Int64Range(900, 3000),
		).Draw(rt, "offset_s")
		action := rapid.SampledFrom([]string{"approve", "veto", "fast-forward", "reality check"}).Draw(rt, "action")

		e := newEnv(true)
		v := e.create(rt, signalsLocked())
		e.clock.AdvanceSeconds(offset)

		ctx := context.Background()
		var err error
		switch action {
		case "approve":
			_, err = e.svc.Approve(ctx, v.ID)
		case "veto":
			_, err = e.svc.Veto(ctx, v.ID)
		case "fast-forward":
			_, err = e.svc.FastForward(ctx, v.ID)
		default:
			_, err = e.svc.RealityCheck(ctx, v.ID, Answers{AnswerNo, AnswerNo, AnswerYes})
		}

		var conflict *ConflictError
		if !errors.As(err, &conflict) || conflict.Current.State != store.StateExpired {
			rt.Fatalf("%s at +%ds = %v, want a conflict on an EXPIRED vault", action, offset, err)
		}
		if rec := e.record(rt, v.ID); rec.State != store.StateExpired || rec.Settled {
			rt.Fatalf("after %s at +%ds the record is %s settled %v, want an unsettled EXPIRED", action, offset, rec.State, rec.Settled)
		}
	})
}

// Feature: cooling-off-vault, Property 9: Reconfirmation settles only an expired vault
//
// **Validates: Requirements 4.4, 4.5, 4.6**
func TestReconfirmSettlesOnlyAnExpiredVault(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		state := stateGen().Draw(rt, "state")

		e := newEnv(false)
		v := e.vaultIn(rt, state)
		before := e.record(rt, v.ID)

		if state == store.StateExpired {
			if !v.ReconfirmNeeded || v.Settled {
				rt.Fatalf("an unsettled EXPIRED vault reports reconfirm_required %v settled %v", v.ReconfirmNeeded, v.Settled)
			}
		}

		got, err := e.svc.Reconfirm(context.Background(), v.ID)
		after := e.record(rt, v.ID)

		if state != store.StateExpired {
			var conflict *ConflictError
			if !errors.As(err, &conflict) || conflict.Current.State != state {
				rt.Fatalf("Reconfirm on a %s vault = %v, want a conflict carrying that state", state, err)
			}
			if !reflect.DeepEqual(after, before) {
				rt.Fatalf("Reconfirm on a %s vault changed the record: %+v -> %+v", state, before, after)
			}
			return
		}

		if err != nil {
			rt.Fatalf("Reconfirm on an EXPIRED vault = %v, want nil", err)
		}
		if got.State != store.StateExpired || !got.Settled || got.ReconfirmNeeded {
			rt.Fatalf("Reconfirm = state %s settled %v reconfirm_required %v, want a settled EXPIRED vault",
				got.State, got.Settled, got.ReconfirmNeeded)
		}
		if after.State != store.StateExpired || !after.Settled {
			rt.Fatalf("stored record after Reconfirm = %s settled %v", after.State, after.Settled)
		}
	})
}
