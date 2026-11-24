package vault

import (
	"time"

	"github.com/RahulKumar1119/rukpay/backend/internal/redact"
	"github.com/RahulKumar1119/rukpay/backend/internal/risk"
	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// Clock returns the current time. The service reads time only through a Clock,
// so tests control it (Requirement 4.7).
type Clock func() time.Time

// TokenIssuer issues the guardian token for a locked vault. guardian.Signer
// implements it. The interface keeps vault from importing guardian.
type TokenIssuer interface {
	Issue(vaultID string, expiry int64) string
}

// Observer is told about every successful change to a vault, including a lazy
// expiry. guardian.Hub implements it. Publish must not block.
type Observer interface {
	Publish(View)
}

// View is the safe projection of a vault record. It is the HTTP response body,
// the SSE payload and the Vault_Event. It holds masked metadata only: no raw
// identifier, OTP, PIN, password or key.
type View struct {
	ID               string           `json:"vault_id"`
	State            store.VaultState `json:"state"`
	Settled          bool             `json:"settled"`
	RiskScore        int              `json:"risk_score"`
	RemainingSeconds int64            `json:"remaining_seconds"`
	ReconfirmNeeded  bool             `json:"reconfirm_required"`
	Reason           string           `json:"reason"`
	Metadata         Metadata         `json:"metadata"`
	// GuardianToken is set only while the vault is in LOCKED_15M.
	GuardianToken string `json:"guardian_token,omitempty"`
	// Evidence is set only once the vault is in VETOED.
	Evidence string `json:"evidence_summary,omitempty"`
	// Explanation is the list of Contributions stored when the vault was
	// created (Requirement 2). It is a pointer to a slice so that encoding/json
	// can tell the three cases apart: a nil pointer leaves the key out (a record
	// written before this field existed), a pointer to an empty slice writes
	// "explanation": [] (a payment that earned no points), and a pointer to a
	// list writes the Contributions in their stored order. The slice behind the
	// pointer is a copy, so a caller cannot reach the stored one through a View.
	Explanation *[]risk.Contribution `json:"explanation,omitempty"`
}

// noopObserver is used when New is given no observer.
type noopObserver struct{}

func (noopObserver) Publish(View) {}

// view projects rec at time now (epoch seconds) into a View.
//
// While the vault is LOCKED_15M it reports the remaining lock seconds and
// recomputes the guardian token from the record's TokenExpiry, so the token is
// never stored (Requirement 7.2). It sets the evidence only for VETOED
// (Requirement 7.3) and ReconfirmNeeded for an EXPIRED vault that is not yet
// settled (Requirement 4.4). Reason and evidence pass through redact.Text.
//
// The Explanation is copied from the record as it is stored. It is never
// computed here, so a read made after the scoring rules or the payer's history
// changed still returns the Contributions of the evaluation (Requirement 2.3).
func (s *Service) view(rec store.VaultRecord, now int64) View {
	v := View{
		ID:        rec.ID,
		State:     rec.State,
		Settled:   rec.Settled,
		RiskScore: rec.RiskScore,
		Reason:    redact.Text(rec.Reason),
		Metadata: Metadata{
			PayerAccountMasked: rec.PayerAccountMasked,
			BeneficiaryMasked:  rec.BeneficiaryMasked,
			UPIMasked:          rec.UPIMasked,
			PhoneMasked:        rec.PhoneMasked,
			AmountINR:          rec.AmountINR,
		},
		Explanation: explanationView(rec.Explanation),
	}

	switch rec.State {
	case store.StateLocked15M:
		if remaining := rec.LockDeadline - now; remaining > 0 {
			v.RemainingSeconds = remaining
		}
		if s.issuer != nil {
			v.GuardianToken = s.issuer.Issue(rec.ID, rec.TokenExpiry)
		}
	case store.StateVetoed:
		v.Evidence = redact.Text(rec.EvidenceSummary)
	case store.StateExpired:
		v.ReconfirmNeeded = !rec.Settled
	}
	return v
}

// explanationView returns the pointer a View holds for a stored Explanation. A
// nil slice means the record has none, and gives a nil pointer so the key is
// left out. Any other slice, empty or not, gives a pointer to a new slice, so
// the pointer is never nil and an empty Explanation is written as [] and never
// as null (Requirements 2.2, 2.5, 2.9).
func explanationView(stored []store.Contribution) *[]risk.Contribution {
	if stored == nil {
		return nil
	}
	list := make([]risk.Contribution, len(stored))
	copy(list, stored)
	return &list
}
