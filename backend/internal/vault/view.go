package vault

import (
	"time"

	"github.com/RahulKumar1119/rukpay/backend/internal/redact"
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

// Metadata holds the masked identifiers and the amount of a vault. It is the
// only part of the payment that a View carries (Requirements 7.1, 8.9).
type Metadata struct {
	PayerAccountMasked string `json:"payer_account_masked,omitempty"`
	BeneficiaryMasked  string `json:"beneficiary_masked,omitempty"`
	UPIMasked          string `json:"upi_masked,omitempty"`
	PhoneMasked        string `json:"phone_masked,omitempty"`
	AmountINR          int64  `json:"amount_inr"`
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
