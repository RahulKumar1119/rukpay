package store

import (
	"errors"
	"strings"
	"testing"
)

// maskField sets one identifier field of a record by name.
type maskField struct {
	name string
	set  func(*VaultRecord, string)
}

var maskFields = []maskField{
	{"PayerAccountMasked", func(r *VaultRecord, v string) { r.PayerAccountMasked = v }},
	{"BeneficiaryMasked", func(r *VaultRecord, v string) { r.BeneficiaryMasked = v }},
	{"PhoneMasked", func(r *VaultRecord, v string) { r.PhoneMasked = v }},
	{"UPIMasked", func(r *VaultRecord, v string) { r.UPIMasked = v }},
}

func TestValidateMaskedAcceptsValidMasks(t *testing.T) {
	tests := []struct {
		field string
		value string
	}{
		{"PayerAccountMasked", "XXXX-XXXX-4821"},
		{"BeneficiaryMasked", "XXXX-XXXX-0000"},
		{"PhoneMasked", "+91-XXXXXX3210"},
		{"UPIMasked", "r***@okaxis"},
		{"UPIMasked", "A***@ok.bank-1"},
	}
	for _, tt := range tests {
		t.Run(tt.field+"/"+tt.value, func(t *testing.T) {
			var rec VaultRecord
			for _, f := range maskFields {
				if f.name == tt.field {
					f.set(&rec, tt.value)
				}
			}
			if err := ValidateMasked(rec); err != nil {
				t.Errorf("ValidateMasked() = %v, want nil", err)
			}
		})
	}
}

func TestValidateMaskedAcceptsAllValidAndEmptyRecords(t *testing.T) {
	tests := []struct {
		name string
		rec  VaultRecord
	}{
		{"all empty", VaultRecord{}},
		{"all valid", VaultRecord{
			PayerAccountMasked: "XXXX-XXXX-4821",
			BeneficiaryMasked:  "XXXX-XXXX-1234",
			PhoneMasked:        "+91-XXXXXX3210",
			UPIMasked:          "r***@okaxis",
		}},
		{"some empty", VaultRecord{PhoneMasked: "+91-XXXXXX3210"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateMasked(tt.rec); err != nil {
				t.Errorf("ValidateMasked() = %v, want nil", err)
			}
		})
	}
}

func TestValidateMaskedRejectsUnmaskedValues(t *testing.T) {
	bad := map[string][]string{
		"PayerAccountMasked": {
			"123456789012",
			"XXXX-XXXX-482",
			"XXXX-XXXX-48211",
			"XXXX-XXXX-48A1",
			"1234-5678-4821",
		},
		"BeneficiaryMasked": {
			"9876543210123",
			"XXXX-4821",
			"xxxx-xxxx-4821",
		},
		"PhoneMasked": {
			"+919876543210",
			"9876543210",
			"+91-XXXXXX321",
			"+91-XXXXX3210",
			"+91-9876543210",
		},
		"UPIMasked": {
			"rahul@okaxis",
			"r**@okaxis",
			"ra***@okaxis",
			"r***@",
			"r***okaxis",
		},
	}
	for _, f := range maskFields {
		for _, v := range bad[f.name] {
			f, v := f, v
			t.Run(f.name+"/"+v, func(t *testing.T) {
				var rec VaultRecord
				f.set(&rec, v)
				err := ValidateMasked(rec)
				if !errors.Is(err, ErrUnmaskedIdentifier) {
					t.Fatalf("ValidateMasked() = %v, want ErrUnmaskedIdentifier", err)
				}
				if strings.Contains(err.Error(), v) {
					t.Errorf("error text %q contains raw value %q", err.Error(), v)
				}
			})
		}
	}
}

func TestValidateMaskedRejectsRawValueAmongValidFields(t *testing.T) {
	rec := VaultRecord{
		PayerAccountMasked: "XXXX-XXXX-4821",
		PhoneMasked:        "+91-XXXXXX3210",
		UPIMasked:          "rahul@okaxis",
	}
	err := ValidateMasked(rec)
	if !errors.Is(err, ErrUnmaskedIdentifier) {
		t.Fatalf("ValidateMasked() = %v, want ErrUnmaskedIdentifier", err)
	}
	if strings.Contains(err.Error(), "rahul") {
		t.Errorf("error text %q contains raw value", err.Error())
	}
}
