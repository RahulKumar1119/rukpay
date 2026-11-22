package vault

import (
	"context"
	"errors"
	"testing"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// permittedPair is the transition table of Requirement 2.1 written out again,
// independently of CanTransition.
func permittedPair(from, to store.VaultState) bool {
	switch {
	case from == store.StateRealityCheck && to == store.StateLocked15M:
		return true
	case from == store.StateLocked15M && to == store.StateGuardianApproved:
		return true
	case from == store.StateLocked15M && to == store.StateVetoed:
		return true
	case from == store.StateLocked15M && to == store.StateExpired:
		return true
	default:
		return false
	}
}

// Feature: cooling-off-vault, Property 3: Transition table is exact
//
// **Validates: Requirements 2.1, 2.2, 2.3, 2.4**
func TestTransitionTableIsExact(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		from := stateGen().Draw(rt, "from")
		to := stateGen().Draw(rt, "to")

		if got, want := CanTransition(from, to), permittedPair(from, to); got != want {
			rt.Fatalf("CanTransition(%s, %s) = %v, want %v", from, to, got, want)
		}
	})
}

// TestTransitionTableCoversEveryPair checks all 36 pairs, so that each one is
// seen on every run, and the three states that allow no move.
func TestTransitionTableCoversEveryPair(t *testing.T) {
	permitted := 0
	for _, from := range allStates() {
		for _, to := range allStates() {
			if CanTransition(from, to) != permittedPair(from, to) {
				t.Errorf("CanTransition(%s, %s) = %v, want %v", from, to, CanTransition(from, to), permittedPair(from, to))
			}
			if CanTransition(from, to) {
				permitted++
			}
		}
	}
	if permitted != 4 {
		t.Errorf("%d pairs are permitted, want exactly 4", permitted)
	}

	for _, terminal := range []store.VaultState{store.StateInstantPass, store.StateGuardianApproved, store.StateVetoed} {
		for _, to := range allStates() {
			if CanTransition(terminal, to) {
				t.Errorf("CanTransition(%s, %s) = true, want false for a terminal state", terminal, to)
			}
		}
	}
}

// rejectedPairs lists every (from, to) that apply must refuse: the pairs
// outside the table, except the in-place changes that REALITY_CHECK, LOCKED_15M
// and EXPIRED allow (a passed check, a fast-forward and a reconfirmation).
func rejectedPairs() [][2]store.VaultState {
	var pairs [][2]store.VaultState
	for _, from := range allStates() {
		for _, to := range allStates() {
			if permittedPair(from, to) || (from == to && canStay(from)) {
				continue
			}
			pairs = append(pairs, [2]store.VaultState{from, to})
		}
	}
	return pairs
}

// Feature: cooling-off-vault, Property 3: Transition table is exact
//
// For any pair outside the table, apply returns ErrInvalidTransition and the
// stored record is unchanged. That includes every move out of INSTANT_PASS,
// GUARDIAN_APPROVED and VETOED, and every change that keeps one of those states.
//
// **Validates: Requirements 2.1, 2.2, 2.3, 2.4**
func TestApplyRejectsPairsOutsideTheTable(t *testing.T) {
	pairs := rejectedPairs()
	rapid.Check(t, func(rt *rapid.T) {
		pair := rapid.SampledFrom(pairs).Draw(rt, "pair")
		from, to := pair[0], pair[1]

		e := newEnv(true)
		v := e.vaultIn(rt, from)
		before := e.record(rt, v.ID)
		writes := e.cs.writeCount()

		_, err := e.svc.apply(context.Background(), v.ID, from, to, nil, func(r *store.VaultRecord) {
			r.Reason = "must not be written"
			r.Settled = !r.Settled
		})
		if !errors.Is(err, ErrInvalidTransition) {
			rt.Fatalf("apply(%s -> %s) = %v, want ErrInvalidTransition", from, to, err)
		}
		if after := e.record(rt, v.ID); after != before {
			rt.Fatalf("apply(%s -> %s) changed the record: %+v -> %+v", from, to, before, after)
		}
		if got := e.cs.writeCount(); got != writes {
			rt.Fatalf("apply(%s -> %s) wrote %d times", from, to, got-writes)
		}
	})
}

// Feature: cooling-off-vault, Property 3: Transition table is exact
//
// Each of the four permitted moves succeeds, changes the state and adds one to
// the version.
//
// **Validates: Requirements 2.1**
func TestApplyPerformsEveryPermittedTransition(t *testing.T) {
	var pairs [][2]store.VaultState
	for _, from := range allStates() {
		for _, to := range allStates() {
			if permittedPair(from, to) {
				pairs = append(pairs, [2]store.VaultState{from, to})
			}
		}
	}

	rapid.Check(t, func(rt *rapid.T) {
		pair := rapid.SampledFrom(pairs).Draw(rt, "pair")
		from, to := pair[0], pair[1]

		e := newEnv(true)
		v := e.vaultIn(rt, from)
		before := e.record(rt, v.ID)

		got, err := e.svc.apply(context.Background(), v.ID, from, to, nil, nil)
		if err != nil {
			rt.Fatalf("apply(%s -> %s) = %v, want nil", from, to, err)
		}
		if got.State != to || got.Version != before.Version+1 {
			rt.Fatalf("apply(%s -> %s) returned state %s version %d, want %s version %d",
				from, to, got.State, got.Version, to, before.Version+1)
		}
		if stored := e.record(rt, v.ID); stored != got {
			rt.Fatalf("stored record %+v differs from the returned record %+v", stored, got)
		}
	})
}
