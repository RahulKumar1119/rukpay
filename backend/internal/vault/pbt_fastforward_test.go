package vault

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// Feature: cooling-off-vault, Property 18: Fast-forward only shortens the lock
//
// With Demo_Mode on, fast-forward on a locked vault sets LockDeadline to
// min(old, now+3), never raises it, and never changes TokenExpiry. When the
// deadline is already within three seconds the record is unchanged and nothing
// is written.
//
// **Validates: Requirements 9.1, 9.4, 9.5, 9.6, 12.8**
func TestFastForwardOnlyShortensTheLock(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		offset := rapid.OneOf(
			rapid.SampledFrom([]int64{0, 1, 896, 897, 898, 899}),
			rapid.Int64Range(0, 899),
		).Draw(rt, "offset_s")
		repeats := rapid.IntRange(1, 3).Draw(rt, "repeats")

		e := newEnv(true)
		v := e.create(rt, signalsLocked())
		e.clock.AdvanceSeconds(offset)
		now := harnessEpoch().Unix() + offset
		before := e.record(rt, v.ID)

		prevDeadline := before.LockDeadline
		for i := 0; i < repeats; i++ {
			writes := e.cs.writeCount()
			rec := e.record(rt, v.ID)

			got, err := e.svc.FastForward(context.Background(), v.ID)
			if err != nil {
				rt.Fatalf("FastForward #%d at +%ds = %v, want nil", i, offset, err)
			}
			after := e.record(rt, v.ID)

			want := rec.LockDeadline
			if target := now + 3; want > target {
				want = target
			}
			if after.LockDeadline != want {
				rt.Fatalf("FastForward #%d: LockDeadline %d, want min(%d, %d) = %d", i, after.LockDeadline, rec.LockDeadline, now+3, want)
			}
			if after.LockDeadline > prevDeadline {
				rt.Fatalf("FastForward #%d raised LockDeadline from %d to %d", i, prevDeadline, after.LockDeadline)
			}
			if after.TokenExpiry != before.TokenExpiry {
				rt.Fatalf("FastForward #%d changed TokenExpiry from %d to %d", i, before.TokenExpiry, after.TokenExpiry)
			}
			if after.State != store.StateLocked15M {
				rt.Fatalf("FastForward #%d left state %s, want LOCKED_15M", i, after.State)
			}
			if got.GuardianToken != (testIssuer{}).Issue(v.ID, before.TokenExpiry) {
				rt.Fatalf("FastForward #%d: token %q changed with the deadline", i, got.GuardianToken)
			}

			if rec.LockDeadline <= now+3 {
				// Already within three seconds: nothing is written.
				if !reflect.DeepEqual(after, rec) || e.cs.writeCount() != writes {
					rt.Fatalf("FastForward #%d changed a deadline that was already within 3 seconds", i)
				}
			}
			prevDeadline = after.LockDeadline
		}
	})
}

// Feature: cooling-off-vault, Property 18: Fast-forward only shortens the lock
//
// For any vault not in LOCKED_15M the result is a conflict that carries the
// current state, and the record is unchanged.
//
// **Validates: Requirements 9.3**
func TestFastForwardConflictsOutsideLockedState(t *testing.T) {
	others := []store.VaultState{
		store.StateInstantPass,
		store.StateRealityCheck,
		store.StateGuardianApproved,
		store.StateVetoed,
		store.StateExpired,
	}
	rapid.Check(t, func(rt *rapid.T) {
		state := rapid.SampledFrom(others).Draw(rt, "state")

		e := newEnv(true)
		v := e.vaultIn(rt, state)
		before := e.record(rt, v.ID)
		writes := e.cs.writeCount()

		_, err := e.svc.FastForward(context.Background(), v.ID)
		var conflict *ConflictError
		if !errors.As(err, &conflict) || conflict.Current.State != state {
			rt.Fatalf("FastForward on a %s vault = %v, want a conflict carrying that state", state, err)
		}
		if after := e.record(rt, v.ID); !reflect.DeepEqual(after, before) || e.cs.writeCount() != writes {
			rt.Fatalf("FastForward on a %s vault changed the store", state)
		}
	})
}

// Feature: cooling-off-vault, Property 18: Fast-forward only shortens the lock
//
// With Demo_Mode off, fast-forward returns ErrNotDemoMode for a vault in any
// state, even one that does not exist, and changes nothing.
//
// **Validates: Requirements 9.2**
func TestFastForwardIsRefusedWithoutDemoMode(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		state := stateGen().Draw(rt, "state")
		missing := rapid.Bool().Draw(rt, "missing")

		e := newEnv(false)
		v := e.vaultIn(rt, state)
		id := v.ID
		if missing {
			id = "00000000000000000000000000000000"
		}
		before := e.record(rt, v.ID)
		writes := e.cs.writeCount()

		_, err := e.svc.FastForward(context.Background(), id)
		if !errors.Is(err, ErrNotDemoMode) {
			rt.Fatalf("FastForward with demo off on a %s vault = %v, want ErrNotDemoMode", state, err)
		}
		if after := e.record(rt, v.ID); !reflect.DeepEqual(after, before) || e.cs.writeCount() != writes {
			rt.Fatalf("FastForward with demo off changed the store")
		}
	})
}
