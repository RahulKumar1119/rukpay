package guardian

import (
	"strings"
	"testing"
	"time"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

func evidenceRecord() store.VaultRecord {
	return store.VaultRecord{
		ID:                 "0123456789abcdef0123456789abcdef",
		State:              store.StateLocked15M,
		RiskScore:          90,
		PayerAccountMasked: "XXXX-XXXX-4821",
		BeneficiaryMasked:  "XXXX-XXXX-1100",
		UPIMasked:          "r***@okaxis",
		PhoneMasked:        "+91-XXXXXX3210",
		AmountINR:          85000,
		CreatedAt:          time.Date(2026, time.November, 21, 23, 45, 0, 0, time.UTC),
	}
}

func TestBuildEvidenceHoldsTheRequiredFacts(t *testing.T) {
	rec := evidenceRecord()
	vetoedAt := time.Date(2026, time.November, 21, 23, 52, 30, 0, time.UTC)

	got := BuildEvidence(rec, vetoedAt)

	for _, want := range []string{
		rec.ID,
		"Amount: INR 85000",
		"Risk score: 90",
		"Payer account: XXXX-XXXX-4821",
		"Beneficiary account: XXXX-XXXX-1100",
		"UPI ID: r***@okaxis",
		"Phone: +91-XXXXXX3210",
		"Payment started: 2026-11-21T23:45:00Z",
		"Stopped at: 2026-11-21T23:52:30Z",
		"National Cybercrime Helpline: 1930",
		"Report online: cybercrime.gov.in",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("evidence does not contain %q:\n%s", want, got)
		}
	}
}

func TestBuildEvidenceSkipsEmptyIdentifiers(t *testing.T) {
	rec := evidenceRecord()
	rec.PayerAccountMasked, rec.BeneficiaryMasked, rec.UPIMasked, rec.PhoneMasked = "", "", "", ""

	got := BuildEvidence(rec, time.Unix(0, 0))

	for _, label := range []string{"Payer account", "Beneficiary account", "UPI ID", "Phone"} {
		if strings.Contains(got, label) {
			t.Errorf("evidence lists %q although the record has no such identifier:\n%s", label, got)
		}
	}
	if !strings.Contains(got, "1930") || !strings.Contains(got, "cybercrime.gov.in") {
		t.Errorf("evidence lacks the helpline details:\n%s", got)
	}
}

// A value that is not masked must never reach the evidence, even if one got into
// the record.
func TestBuildEvidenceLeavesOutUnmaskedIdentifiers(t *testing.T) {
	rec := evidenceRecord()
	rec.PayerAccountMasked = "123456789012"
	rec.BeneficiaryMasked = "XXXX-XXXX-12"
	rec.UPIMasked = "rahul@okaxis"
	rec.PhoneMasked = "9876543210"

	got := BuildEvidence(rec, time.Unix(0, 0))

	for _, raw := range []string{"123456789012", "XXXX-XXXX-12", "rahul@okaxis", "9876543210"} {
		if strings.Contains(got, raw) {
			t.Errorf("evidence contains the unmasked value %q:\n%s", raw, got)
		}
	}
	for _, label := range []string{"Payer account", "Beneficiary account", "UPI ID", "Phone"} {
		if strings.Contains(got, label) {
			t.Errorf("evidence lists %q for an unmasked value:\n%s", label, got)
		}
	}
}

func TestBuildEvidenceRedactsOTPPINAndPasswordPatterns(t *testing.T) {
	rec := evidenceRecord()
	rec.ID = "otp=482913"

	got := BuildEvidence(rec, time.Unix(0, 0))

	if strings.Contains(got, "482913") {
		t.Errorf("evidence still holds the OTP value:\n%s", got)
	}
	if !strings.Contains(got, "Vault: [REDACTED]") {
		t.Errorf("evidence does not show [REDACTED] in place of the OTP:\n%s", got)
	}
}

func TestBuildEvidenceUsesUTC(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+1800)
	rec := evidenceRecord()
	rec.CreatedAt = time.Date(2026, time.November, 22, 5, 15, 0, 0, ist) // 23:45 UTC the day before

	got := BuildEvidence(rec, time.Date(2026, time.November, 22, 5, 22, 30, 0, ist))

	if !strings.Contains(got, "Payment started: 2026-11-21T23:45:00Z") || !strings.Contains(got, "Stopped at: 2026-11-21T23:52:30Z") {
		t.Errorf("evidence times are not in UTC:\n%s", got)
	}
}

// Redaction is eager by design: a masked value that spells a field name is
// redacted with it, because the alternative is a risk of leaking a real one.
func TestBuildEvidenceRedactsAMaskedValueThatSpellsAFieldName(t *testing.T) {
	rec := evidenceRecord()
	rec.UPIMasked = "r***@otp.bank"

	got := BuildEvidence(rec, time.Unix(0, 0))

	if strings.Contains(got, "otp.bank") {
		t.Errorf("evidence still holds the value that spells a field name:\n%s", got)
	}
	if !strings.Contains(got, "UPI ID: r***@[REDACTED]") {
		t.Errorf("evidence does not show the redaction in place:\n%s", got)
	}
}
