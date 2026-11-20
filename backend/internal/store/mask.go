package store

import "regexp"

// Masked identifier formats (Requirement 12.3).
var (
	// reAccount matches a masked account number such as XXXX-XXXX-4821.
	reAccount = regexp.MustCompile(`^XXXX-XXXX-\d{4}$`)
	// rePhone matches a masked phone number such as +91-XXXXXX3210.
	rePhone = regexp.MustCompile(`^\+91-X{6}\d{4}$`)
	// reUPI matches a masked UPI ID such as r***@okaxis.
	reUPI = regexp.MustCompile(`^[A-Za-z0-9]\*\*\*@[A-Za-z0-9.\-]+$`)
)

// ValidateMasked returns ErrUnmaskedIdentifier when a non-empty identifier
// field of rec does not match its mask format. Empty fields are allowed
// because Signals carry no identifiers (Requirement 12.4).
//
// The returned error is the bare sentinel and never contains a field value
// (Requirement 12.6). Adapters wrap it in an opError.
func ValidateMasked(rec VaultRecord) error {
	checks := []struct {
		value string
		re    *regexp.Regexp
	}{
		{rec.PayerAccountMasked, reAccount},
		{rec.BeneficiaryMasked, reAccount},
		{rec.PhoneMasked, rePhone},
		{rec.UPIMasked, reUPI},
	}
	for _, c := range checks {
		if c.value != "" && !c.re.MatchString(c.value) {
			return ErrUnmaskedIdentifier
		}
	}
	return nil
}
