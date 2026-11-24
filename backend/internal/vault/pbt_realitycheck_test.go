package vault

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// Feature: cooling-off-vault, Property 6: Reality check outcome
//
// **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 12.7**
func TestRealityCheckOutcomeFollowsTheAnswers(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		a := answersGen().Draw(rt, "answers")
		offset := rapid.Int64Range(0, 3000).Draw(rt, "clock_offset_s")

		e := newEnv(false)
		v := e.create(rt, signalsRealityCheck())
		e.clock.AdvanceSeconds(offset)
		now := harnessEpoch().Unix() + offset

		got, err := e.svc.RealityCheck(context.Background(), v.ID, a)
		if err != nil {
			rt.Fatalf("RealityCheck(%+v) = %v, want nil", a, err)
		}
		rec := e.record(rt, v.ID)

		if wantLock(a) {
			if got.State != store.StateLocked15M || rec.State != store.StateLocked15M {
				rt.Fatalf("answers %+v gave state %s, want LOCKED_15M", a, got.State)
			}
			if rec.LockDeadline != now+LockDuration || rec.TokenExpiry != now+LockDuration {
				rt.Fatalf("LockDeadline %d TokenExpiry %d, want both %d", rec.LockDeadline, rec.TokenExpiry, now+LockDuration)
			}
			if rec.ExpiresAt.Unix() < rec.LockDeadline+3600 {
				rt.Fatalf("ExpiresAt %d is earlier than the deadline plus 3600", rec.ExpiresAt.Unix())
			}
			if want := (testIssuer{}).Issue(v.ID, now+LockDuration); got.GuardianToken != want {
				rt.Fatalf("GuardianToken = %q, want %q", got.GuardianToken, want)
			}
			if got.Settled || rec.Settled || got.RemainingSeconds != LockDuration {
				rt.Fatalf("locked vault: settled %v/%v remaining %d", got.Settled, rec.Settled, got.RemainingSeconds)
			}
			return
		}

		if got.State != store.StateRealityCheck || rec.State != store.StateRealityCheck {
			rt.Fatalf("answers %+v gave state %s, want REALITY_CHECK", a, got.State)
		}
		if !got.Settled || !rec.Settled {
			rt.Fatalf("calm answers %+v left Settled false", a)
		}
		if rec.LockDeadline != 0 || got.GuardianToken != "" {
			rt.Fatalf("calm answers locked the vault: deadline %d token %q", rec.LockDeadline, got.GuardianToken)
		}
	})
}

// TestRealityCheckAllEightCombinations runs every answer combination once, so
// each is seen on every run: the vault locks for seven of them and passes for
// the one calm combination.
func TestRealityCheckAllEightCombinations(t *testing.T) {
	locks, passes := 0, 0
	for _, coercion := range []Answer{AnswerYes, AnswerNo} {
		for _, secrecy := range []Answer{AnswerYes, AnswerNo} {
			for _, verification := range []Answer{AnswerYes, AnswerNo} {
				a := Answers{Coercion: coercion, Secrecy: secrecy, Verification: verification}
				e := newEnv(false)
				v := e.create(t, signalsRealityCheck())
				got, err := e.svc.RealityCheck(context.Background(), v.ID, a)
				if err != nil {
					t.Fatalf("RealityCheck(%+v) = %v, want nil", a, err)
				}
				if wantLock(a) {
					locks++
					if got.State != store.StateLocked15M {
						t.Errorf("answers %+v gave %s, want LOCKED_15M", a, got.State)
					}
				} else {
					passes++
					if got.State != store.StateRealityCheck || !got.Settled {
						t.Errorf("answers %+v gave %s settled %v, want a settled REALITY_CHECK", a, got.State, got.Settled)
					}
				}
			}
		}
	}
	if locks != 7 || passes != 1 {
		t.Errorf("%d locking and %d passing combinations, want 7 and 1", locks, passes)
	}
}

// invalidAnswerGen draws a value that is not YES or NO.
func invalidAnswerGen() *rapid.Generator[Answer] {
	return rapid.OneOf(
		rapid.SampledFrom([]Answer{"", "yes", "no", "Yes", "No", "MAYBE", "Y", "N", " YES", "YES ", "TRUE"}),
		rapid.Custom(func(t *rapid.T) Answer {
			return Answer(rapid.String().Draw(t, "text"))
		}).Filter(func(a Answer) bool { return a != AnswerYes && a != AnswerNo }),
	)
}

// Feature: cooling-off-vault, Property 7: Bad reality-check requests change nothing
//
// An answer that is missing or is not YES or NO is rejected, and the stored
// record is unchanged.
//
// **Validates: Requirements 3.7, 3.8**
func TestRealityCheckRejectsInvalidAnswers(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		valid := answersGen().Draw(rt, "valid")
		bad := invalidAnswerGen().Draw(rt, "invalid")
		which := rapid.IntRange(0, 2).Draw(rt, "which")

		a := valid
		switch which {
		case 0:
			a.Coercion = bad
		case 1:
			a.Secrecy = bad
		default:
			a.Verification = bad
		}

		e := newEnv(false)
		v := e.create(rt, signalsRealityCheck())
		before := e.record(rt, v.ID)
		writes := e.cs.writeCount()

		_, err := e.svc.RealityCheck(context.Background(), v.ID, a)
		if !errors.Is(err, ErrInvalidAnswers) {
			rt.Fatalf("RealityCheck(%+v) = %v, want ErrInvalidAnswers", a, err)
		}
		if after := e.record(rt, v.ID); !reflect.DeepEqual(after, before) {
			rt.Fatalf("a rejected check changed the record: %+v -> %+v", before, after)
		}
		if e.cs.writeCount() != writes {
			rt.Fatalf("a rejected check wrote to the store")
		}
	})
}

// Feature: cooling-off-vault, Property 7: Bad reality-check requests change nothing
//
// A vault that is not in REALITY_CHECK answers with a conflict that carries its
// current state, and nothing changes.
//
// **Validates: Requirements 3.7, 3.8**
func TestRealityCheckConflictsOutsideRealityCheck(t *testing.T) {
	others := []store.VaultState{
		store.StateInstantPass,
		store.StateLocked15M,
		store.StateGuardianApproved,
		store.StateVetoed,
		store.StateExpired,
	}
	rapid.Check(t, func(rt *rapid.T) {
		state := rapid.SampledFrom(others).Draw(rt, "state")
		a := answersGen().Draw(rt, "answers")

		e := newEnv(false)
		v := e.vaultIn(rt, state)
		before := e.record(rt, v.ID)
		writes := e.cs.writeCount()

		_, err := e.svc.RealityCheck(context.Background(), v.ID, a)
		var conflict *ConflictError
		if !errors.As(err, &conflict) {
			rt.Fatalf("RealityCheck on a %s vault = %v, want *ConflictError", state, err)
		}
		if conflict.Current.State != state || conflict.Current.ID != v.ID {
			rt.Fatalf("conflict carries state %s id %s, want %s %s", conflict.Current.State, conflict.Current.ID, state, v.ID)
		}
		if after := e.record(rt, v.ID); !reflect.DeepEqual(after, before) {
			rt.Fatalf("a conflicting check changed the record: %+v -> %+v", before, after)
		}
		if e.cs.writeCount() != writes {
			rt.Fatalf("a conflicting check wrote to the store")
		}
	})
}

// Feature: cooling-off-vault, Property 7: Bad reality-check requests change nothing
//
// A REALITY_CHECK vault that already passed its check rejects a second
// submission, whatever the answers, and nothing changes.
//
// **Validates: Requirements 3.8**
func TestRealityCheckRejectsASecondSubmission(t *testing.T) {
	calm := Answers{Coercion: AnswerNo, Secrecy: AnswerNo, Verification: AnswerYes}
	rapid.Check(t, func(rt *rapid.T) {
		second := answersGen().Draw(rt, "second")

		e := newEnv(false)
		v := e.create(rt, signalsRealityCheck())
		if _, err := e.svc.RealityCheck(context.Background(), v.ID, calm); err != nil {
			rt.Fatalf("first check = %v, want nil", err)
		}
		before := e.record(rt, v.ID)

		_, err := e.svc.RealityCheck(context.Background(), v.ID, second)
		var conflict *ConflictError
		if !errors.As(err, &conflict) {
			rt.Fatalf("second check %+v = %v, want *ConflictError", second, err)
		}
		if after := e.record(rt, v.ID); !reflect.DeepEqual(after, before) {
			rt.Fatalf("a second check changed the record: %+v -> %+v", before, after)
		}
	})
}
