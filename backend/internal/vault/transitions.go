package vault

import (
	"errors"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

var (
	// ErrInvalidTransition is returned when a change is requested that the
	// transition table does not allow. The stored record is left unchanged
	// (Requirement 2.3).
	ErrInvalidTransition = errors.New("invalid transition")

	// ErrNotDemoMode is returned by FastForward when Demo_Mode is off
	// (Requirement 9.2).
	ErrNotDemoMode = errors.New("demo mode disabled")
)

// ConflictError is returned when the vault is not in the state an action needs,
// or when UpdateIf lost a race to another caller (Requirements 2.6, 3.8, 4.6,
// 6.6). It carries the current View, so a client that lost can update its screen
// without another request.
type ConflictError struct {
	Current View
}

// Error returns a fixed message. It carries no vault data.
func (e *ConflictError) Error() string { return "vault state conflict" }

// allowed lists the states a vault in from may move to (Requirement 2.1).
// Creation into INSTANT_PASS, REALITY_CHECK or LOCKED_15M is not a transition.
// It happens only in Evaluate, which takes the state from risk.Evaluate.
func allowed(from store.VaultState) []store.VaultState {
	switch from {
	case store.StateRealityCheck:
		return []store.VaultState{store.StateLocked15M}
	case store.StateLocked15M:
		return []store.VaultState{store.StateGuardianApproved, store.StateVetoed, store.StateExpired}
	default:
		return nil
	}
}

// CanTransition reports whether a vault may move from one state to another.
// Exactly four moves are allowed: REALITY_CHECK to LOCKED_15M, and LOCKED_15M to
// GUARDIAN_APPROVED, VETOED or EXPIRED (Requirement 2.1). INSTANT_PASS,
// GUARDIAN_APPROVED and VETOED allow no move at all (Requirement 2.4).
func CanTransition(from, to store.VaultState) bool {
	for _, target := range allowed(from) {
		if target == to {
			return true
		}
	}
	return false
}

// canStay reports whether a record in state may be changed without leaving the
// state: the reality check settles in REALITY_CHECK, fast-forward shortens a
// LOCKED_15M deadline, and reconfirmation settles EXPIRED. The three terminal
// states allow no change at all.
func canStay(state store.VaultState) bool {
	switch state {
	case store.StateRealityCheck, store.StateLocked15M, store.StateExpired:
		return true
	default:
		return false
	}
}
