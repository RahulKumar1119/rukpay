package vault

import "github.com/RahulKumar1119/rukpay/backend/internal/risk"

// EvaluateRequest is the body of POST /api/v1/transactions/evaluate: the five
// risk signals and the optional masked identifiers.
//
// The embedded risk.Signals has no JSON tag, so its five fields appear at the
// top level of the body: amount_inr, beneficiary_age_hours, is_late_night,
// active_unknown_call and rapid_retry_count. Service.Evaluate reads them
// through req.Signals.
//
// The type has no field for a raw identifier, an OTP, a PIN or a password. A
// field the type does not declare is dropped when the body is decoded, so it
// cannot be stored, logged or published (Requirement 11.3). Service.Evaluate
// checks the four identifiers with store.ValidateMasked before it creates a
// vault (Requirement 1.6).
type EvaluateRequest struct {
	risk.Signals
	PayerAccountMasked string `json:"payer_account_masked,omitempty"`
	BeneficiaryMasked  string `json:"beneficiary_masked,omitempty"`
	UPIMasked          string `json:"upi_masked,omitempty"`
	PhoneMasked        string `json:"phone_masked,omitempty"`
}

// Metadata holds the masked identifiers and the amount of a vault. It is the
// only part of the payment that a View carries (Requirements 7.1, 8.9).
//
// amount_inr is always present. The four identifiers are left out of the JSON
// when empty.
type Metadata struct {
	PayerAccountMasked string `json:"payer_account_masked,omitempty"`
	BeneficiaryMasked  string `json:"beneficiary_masked,omitempty"`
	UPIMasked          string `json:"upi_masked,omitempty"`
	PhoneMasked        string `json:"phone_masked,omitempty"`
	AmountINR          int64  `json:"amount_inr"`
}
