package vault

import (
	"context"
	"errors"
	"testing"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// seqOp is one step of a generated operation sequence.
type seqOp struct {
	kind    string // check, approve, veto, fast-forward, get, reconfirm or advance
	answers Answers
	secs    int64
}

func seqOpGen() *rapid.Generator[seqOp] {
	return rapid.Custom(func(t *rapid.T) seqOp {
		return seqOp{
			kind: rapid.SampledFrom([]string{
				"check", "approve", "veto", "fast-forward", "get", "reconfirm", "advance", "advance",
			}).Draw(t, "kind"),
			answers: answersGen().Draw(t, "answers"),
			secs: rapid.OneOf(
				rapid.SampledFrom([]int64{0, 1, 2, 3, 4, 5, 899, 900, 901}),
				rapid.Int64Range(0, 1500),
			).Draw(t, "secs"),
		}
	})
}

// seqModel is a plain model of the vault rules, kept beside the real service.
// It is written from the requirements, not from the service code.
type seqModel struct {
	state       store.VaultState
	settled     bool
	deadline    int64
	now         int64
	rcPassed    bool // a reality check has passed
	reconfirmed bool // an EXPIRED vault has been reconfirmed
}

// lazy applies the expiry that every read or action performs first.
func (m *seqModel) lazy() {
	if m.state == store.StateLocked15M && m.now >= m.deadline {
		m.state = store.StateExpired
		m.settled = false
	}
}

// step applies op to the model and reports whether the service must accept it.
func (m *seqModel) step(op seqOp) (accepted bool) {
	if op.kind == "advance" {
		m.now += op.secs
		return true
	}
	m.lazy()

	switch op.kind {
	case "check":
		if m.state != store.StateRealityCheck || m.settled {
			return false
		}
		if wantLock(op.answers) {
			m.state = store.StateLocked15M
			m.deadline = m.now + 900
			m.settled = false
		} else {
			m.settled = true
			m.rcPassed = true
		}
		return true
	case "approve":
		if m.state != store.StateLocked15M {
			return false
		}
		m.state, m.settled = store.StateGuardianApproved, true
		return true
	case "veto":
		if m.state != store.StateLocked15M {
			return false
		}
		m.state, m.settled = store.StateVetoed, false
		return true
	case "fast-forward":
		if m.state != store.StateLocked15M {
			return false
		}
		if target := m.now + 3; m.deadline > target {
			m.deadline = target
		}
		return true
	case "reconfirm":
		if m.state != store.StateExpired {
			return false
		}
		m.settled = true
		m.reconfirmed = true
		return true
	default: // get
		return true
	}
}

// settledIsPermitted is Requirement 12.4: Settled is true only in INSTANT_PASS,
// GUARDIAN_APPROVED, a REALITY_CHECK vault after a passed check, or an EXPIRED
// vault after reconfirmation.
func (m *seqModel) settledIsPermitted(state store.VaultState, settled bool) bool {
	if !settled {
		return true
	}
	switch state {
	case store.StateInstantPass, store.StateGuardianApproved:
		return true
	case store.StateRealityCheck:
		return m.rcPassed
	case store.StateExpired:
		return m.reconfirmed
	default:
		return false
	}
}

// Feature: cooling-off-vault, Property 4: Operation sequences only make permitted state changes
// Feature: cooling-off-vault, Property 10: Settled only where permitted
//
// A model of the rules runs beside the real service over a generated sequence of
// operations. After each operation the stored state and Settled flag match the
// model, every state change is a permitted one, a terminal vault never changes,
// and Settled is true only where it is allowed.
//
// **Validates: Requirements 2.1, 2.4, 12.2, 12.3, 12.4**
func TestOperationSequencesOnlyMakePermittedChanges(t *testing.T) {
	starts := []store.VaultState{store.StateInstantPass, store.StateRealityCheck, store.StateLocked15M}

	rapid.Check(t, func(rt *rapid.T) {
		start := rapid.SampledFrom(starts).Draw(rt, "start")
		ops := rapid.SliceOfN(seqOpGen(), 1, 30).Draw(rt, "ops")

		e := newEnv(true)
		v := e.vaultIn(rt, start)
		ctx := context.Background()

		m := &seqModel{
			state:    start,
			settled:  start == store.StateInstantPass,
			deadline: e.record(rt, v.ID).LockDeadline,
			now:      harnessEpoch().Unix(),
		}

		// The records expire an hour after the moment they must exist, so the
		// clock may move only so far.
		budget := int64(3000)
		prev := start

		for i, op := range ops {
			if op.kind == "advance" {
				if op.secs > budget {
					op.secs = budget
				}
				budget -= op.secs
				e.clock.AdvanceSeconds(op.secs)
			}
			wantOK := m.step(op)

			var (
				got View
				err error
			)
			switch op.kind {
			case "check":
				got, err = e.svc.RealityCheck(ctx, v.ID, op.answers)
			case "approve":
				got, err = e.svc.Approve(ctx, v.ID)
			case "veto":
				got, err = e.svc.Veto(ctx, v.ID)
			case "fast-forward":
				got, err = e.svc.FastForward(ctx, v.ID)
			case "reconfirm":
				got, err = e.svc.Reconfirm(ctx, v.ID)
			case "get":
				got, err = e.svc.Get(ctx, v.ID)
			default:
				continue // advance: nothing is read, so nothing is compared
			}

			var conflict *ConflictError
			switch {
			case wantOK && err != nil:
				rt.Fatalf("op %d %s: got %v, want success (model state %s)", i, op.kind, err, m.state)
			case !wantOK && !errors.As(err, &conflict):
				rt.Fatalf("op %d %s: got %v, want *ConflictError (model state %s)", i, op.kind, err, m.state)
			}
			if wantOK && (got.State != m.state || got.Settled != m.settled) {
				rt.Fatalf("op %d %s: View = %s settled %v, model = %s settled %v",
					i, op.kind, got.State, got.Settled, m.state, m.settled)
			}

			rec := e.record(rt, v.ID)
			if rec.State != m.state || rec.Settled != m.settled {
				rt.Fatalf("op %d %s: stored %s settled %v, model %s settled %v",
					i, op.kind, rec.State, rec.Settled, m.state, m.settled)
			}
			if op.kind == "fast-forward" && wantOK && rec.LockDeadline != m.deadline {
				rt.Fatalf("op %d fast-forward: LockDeadline %d, model %d", i, rec.LockDeadline, m.deadline)
			}

			// Property 4: only permitted changes, and terminal states stay put.
			if rec.State != prev && !CanTransition(prev, rec.State) {
				rt.Fatalf("op %d %s: state moved %s -> %s, which is not permitted", i, op.kind, prev, rec.State)
			}
			switch prev {
			case store.StateGuardianApproved, store.StateVetoed, store.StateInstantPass:
				if rec.State != prev {
					rt.Fatalf("op %d %s: terminal state %s changed to %s", i, op.kind, prev, rec.State)
				}
			}
			prev = rec.State

			// Property 10: Settled only where permitted.
			if !m.settledIsPermitted(rec.State, rec.Settled) {
				rt.Fatalf("op %d %s: Settled is true in %s without a passed check or reconfirmation", i, op.kind, rec.State)
			}
		}
	})
}
