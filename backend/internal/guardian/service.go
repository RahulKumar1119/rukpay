package guardian

import (
	"context"
	"errors"
	"time"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
	"github.com/RahulKumar1119/rukpay/backend/internal/vault"
)

// Guardian action names.
const (
	ActionApprove = "APPROVE"
	ActionVeto    = "VETO"
)

// Result statuses.
const (
	// StatusOK means the action was carried out.
	StatusOK = "OK"
	// StatusConfirmationRequired means an APPROVE arrived without the second
	// confirming tap, so nothing changed (Requirement 6.2).
	StatusConfirmationRequired = "CONFIRMATION_REQUIRED"
)

// ErrInvalidAction is returned for an action that is neither APPROVE nor VETO
// (Requirement 6.5).
var ErrInvalidAction = errors.New("invalid action")

// Action is the body of a guardian action request. Action is APPROVE or VETO.
// Confirmed is the second tap that an APPROVE needs. A VETO never needs it.
type Action struct {
	Action    string `json:"action"`
	Token     string `json:"token"`
	Confirmed bool   `json:"confirmed"`
}

// Result is the answer to an action: a status and the current View of the vault.
type Result struct {
	Status string     `json:"status"`
	View   vault.View `json:"vault"`
}

// Service runs guardian actions. Create it with NewService.
type Service struct {
	vault  *vault.Service
	signer *Signer
	clock  vault.Clock
}

// NewService returns a Service over v. It sets BuildEvidence as the evidence
// builder of v, so a vetoed vault stores the full evidence summary. A nil clock
// means time.Now.
func NewService(v *vault.Service, signer *Signer, clock vault.Clock) *Service {
	if clock == nil {
		clock = time.Now
	}
	v.SetEvidenceBuilder(BuildEvidence)
	return &Service{vault: v, signer: signer, clock: clock}
}

// Act carries out a guardian action on the vault id from the request path.
//
// It verifies the token first, before it reads the action or touches the store.
// A failed check returns ErrForbidden and makes no store call at all
// (Requirements 5.3, 5.4). Then:
//
//   - an action other than APPROVE or VETO returns ErrInvalidAction;
//   - APPROVE without Confirmed returns StatusConfirmationRequired with the
//     current View and writes nothing (Requirement 6.2);
//   - APPROVE with Confirmed approves the vault (Requirement 6.1);
//   - VETO vetoes it and stores the evidence summary (Requirement 6.3). The user
//     panel vetoes through the same call, with the token from its own read
//     (Requirement 6.7).
//
// A vault that is not in LOCKED_15M, including one whose deadline has passed,
// gives a *vault.ConflictError that carries its current state, for every action
// (Requirement 6.6).
func (s *Service) Act(ctx context.Context, id string, a Action) (Result, error) {
	if err := s.signer.Verify(a.Token, id, s.clock().Unix()); err != nil {
		return Result{}, err
	}

	switch a.Action {
	case ActionApprove:
		if !a.Confirmed {
			return s.needConfirmation(ctx, id)
		}
		v, err := s.vault.Approve(ctx, id)
		return s.result(v, err)
	case ActionVeto:
		v, err := s.vault.Veto(ctx, id)
		return s.result(v, err)
	default:
		return Result{}, ErrInvalidAction
	}
}

// needConfirmation answers an unconfirmed APPROVE. It reads the vault, which may
// apply lazy expiry, and changes nothing else. A vault that is not locked is a
// conflict, as it would be for a confirmed APPROVE.
func (s *Service) needConfirmation(ctx context.Context, id string) (Result, error) {
	v, err := s.vault.Get(ctx, id)
	if err != nil {
		return Result{}, err
	}
	if v.State != store.StateLocked15M {
		return Result{}, &vault.ConflictError{Current: v}
	}
	return Result{Status: StatusConfirmationRequired, View: v}, nil
}

func (s *Service) result(v vault.View, err error) (Result, error) {
	if err != nil {
		return Result{}, err
	}
	return Result{Status: StatusOK, View: v}, nil
}
