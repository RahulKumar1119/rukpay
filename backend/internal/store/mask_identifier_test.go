package store

import (
	"errors"
	"strings"
	"testing"
)

func TestMaskedFormat(t *testing.T) {
	tests := []struct {
		value string
		want  MaskFormat
	}{
		{"XXXX-XXXX-4821", MaskAccount},
		{"XXXX-XXXX-0000", MaskAccount},
		{"+91-XXXXXX3210", MaskPhone},
		{"r***@okaxis", MaskUPI},
		{"A***@ok.bank-1", MaskUPI},

		{"", MaskNone},
		{" ", MaskNone},
		{"123456789012", MaskNone},
		{"XXXX-XXXX-482", MaskNone},
		{"XXXX-XXXX-48211", MaskNone},
		{"xxxx-xxxx-4821", MaskNone},
		{"+919876543210", MaskNone},
		{"+91-XXXXX3210", MaskNone},
		{"rahul@okaxis", MaskNone},
		{"r**@okaxis", MaskNone},
		{"r***@", MaskNone},
		// Anchoring: padding and a trailing newline are not masked.
		{" XXXX-XXXX-4821", MaskNone},
		{"XXXX-XXXX-4821 ", MaskNone},
		{"XXXX-XXXX-4821\n", MaskNone},
		{"r***@okaxis\n", MaskNone},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			if got := MaskedFormat(tt.value); got != tt.want {
				t.Errorf("MaskedFormat(%q) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}

func TestValidateMaskedIdentifier(t *testing.T) {
	tests := []struct {
		value   string
		wantErr bool
	}{
		{"XXXX-XXXX-4821", false},
		{"+91-XXXXXX3210", false},
		{"r***@okaxis", false},
		{"", true},
		{"9876543210123", true},
		{"+919876543210", true},
		{"rahul@okaxis", true},
		{"XXXX-XXXX-4821\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			err := ValidateMaskedIdentifier(tt.value)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("ValidateMaskedIdentifier(%q) = %v, want nil", tt.value, err)
				}
				return
			}
			if !errors.Is(err, ErrUnmaskedIdentifier) {
				t.Fatalf("ValidateMaskedIdentifier(%q) = %v, want ErrUnmaskedIdentifier", tt.value, err)
			}
			if tt.value != "" && strings.Contains(err.Error(), tt.value) {
				t.Errorf("error text %q contains the value", err.Error())
			}
		})
	}
}

// TestMaskedFormatAgreesWithValidateMasked keeps the new helpers on the same
// three patterns as ValidateMasked: a non-empty value is a masked identifier
// exactly when at least one identifier field of a VaultRecord accepts it, and
// the format names the field that accepts it.
func TestMaskedFormatAgreesWithValidateMasked(t *testing.T) {
	values := []string{
		"XXXX-XXXX-4821", "+91-XXXXXX3210", "r***@okaxis", "A***@ok.bank-1",
		"123456789012", "XXXX-XXXX-482", "+919876543210", "rahul@okaxis",
		"r**@okaxis", " XXXX-XXXX-4821", "XXXX-XXXX-4821\n", "x",
	}
	for _, v := range values {
		var accepted [4]bool
		for i, f := range maskFields {
			var rec VaultRecord
			f.set(&rec, v)
			accepted[i] = ValidateMasked(rec) == nil
		}
		// maskFields order: payer account, beneficiary account, phone, UPI.
		wantFormat := MaskNone
		switch {
		case accepted[0] && accepted[1]:
			wantFormat = MaskAccount
		case accepted[2]:
			wantFormat = MaskPhone
		case accepted[3]:
			wantFormat = MaskUPI
		}
		if got := MaskedFormat(v); got != wantFormat {
			t.Errorf("MaskedFormat(%q) = %d, but ValidateMasked accepts it as format %d", v, got, wantFormat)
		}
		if (ValidateMaskedIdentifier(v) == nil) != (wantFormat != MaskNone) {
			t.Errorf("ValidateMaskedIdentifier(%q) disagrees with ValidateMasked", v)
		}
	}
}
