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

// MaskFormat names the masked-identifier format that a string matches.
type MaskFormat int

// Masked-identifier formats. MaskNone means the string matches none of them.
const (
	MaskNone MaskFormat = iota
	MaskAccount
	MaskPhone
	MaskUPI
)

// MaskedFormat reports which masked format v matches, using the same three
// patterns as ValidateMasked. It returns MaskNone for any other string,
// including the empty string. The patterns are anchored, so surrounding spaces
// or a trailing newline make a value unmasked.
func MaskedFormat(v string) MaskFormat {
	switch {
	case reAccount.MatchString(v):
		return MaskAccount
	case rePhone.MatchString(v):
		return MaskPhone
	case reUPI.MatchString(v):
		return MaskUPI
	}
	return MaskNone
}

// ValidateMaskedIdentifier returns ErrUnmaskedIdentifier unless v matches one
// of the three masked formats. It rejects the empty string, because a caller
// that allows an empty identifier (a History_Event with no recipient) must
// check for emptiness itself. The error is the bare sentinel and never holds
// v. Adapters wrap it in an opError.
func ValidateMaskedIdentifier(v string) error {
	if MaskedFormat(v) == MaskNone {
		return ErrUnmaskedIdentifier
	}
	return nil
}

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
