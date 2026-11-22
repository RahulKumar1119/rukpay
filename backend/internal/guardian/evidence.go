package guardian

import (
	"strconv"
	"strings"
	"time"

	"github.com/RahulKumar1119/rukpay/backend/internal/redact"
	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// Helpline details printed on every evidence summary (Requirement 6.4).
const (
	helplineLine     = "National Cybercrime Helpline: 1930"
	reportOnlineLine = "Report online: cybercrime.gov.in"
	evidenceTitle    = "Payment stopped by RukPay"
)

// BuildEvidence writes the evidence summary for a vetoed vault: the vault id,
// the masked identifiers, the amount, the risk score, when the payment started,
// when it was stopped, and the National Cybercrime Helpline details
// (Requirements 6.3, 6.4).
//
// It reads only fields of the record, which holds masked values. It adds an
// identifier only when the value passes store.ValidateMasked, so a value that is
// not masked is left out even if one reached the record. The whole text then
// passes through redact.Text, so an OTP, PIN or password field becomes
// [REDACTED] (Requirement 6.8). Redaction is deliberately eager: a masked value
// that happens to spell one of those words, such as a UPI handle "r***@otp.bank",
// is redacted as well.
//
// It matches vault.EvidenceBuilder.
func BuildEvidence(rec store.VaultRecord, vetoedAt time.Time) string {
	lines := []string{
		evidenceTitle,
		"Vault: " + rec.ID,
		"Amount: INR " + strconv.FormatInt(rec.AmountINR, 10),
		"Risk score: " + strconv.Itoa(rec.RiskScore),
	}

	identifiers := []struct {
		label string
		value string
		field func(string) store.VaultRecord
	}{
		{"Payer account", rec.PayerAccountMasked, func(v string) store.VaultRecord { return store.VaultRecord{PayerAccountMasked: v} }},
		{"Beneficiary account", rec.BeneficiaryMasked, func(v string) store.VaultRecord { return store.VaultRecord{BeneficiaryMasked: v} }},
		{"UPI ID", rec.UPIMasked, func(v string) store.VaultRecord { return store.VaultRecord{UPIMasked: v} }},
		{"Phone", rec.PhoneMasked, func(v string) store.VaultRecord { return store.VaultRecord{PhoneMasked: v} }},
	}
	for _, id := range identifiers {
		if id.value == "" || store.ValidateMasked(id.field(id.value)) != nil {
			continue
		}
		lines = append(lines, id.label+": "+id.value)
	}

	lines = append(lines,
		"Payment started: "+rec.CreatedAt.UTC().Format(time.RFC3339),
		"Stopped at: "+vetoedAt.UTC().Format(time.RFC3339),
		helplineLine,
		reportOnlineLine,
	)
	return redact.Text(strings.Join(lines, "\n"))
}
