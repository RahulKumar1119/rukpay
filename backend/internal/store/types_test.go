package store

import (
	"reflect"
	"strings"
	"testing"
)

func TestVaultStateConstantsEqualTheirNames(t *testing.T) {
	tests := []struct {
		name  string
		state VaultState
	}{
		{"INSTANT_PASS", StateInstantPass},
		{"REALITY_CHECK", StateRealityCheck},
		{"LOCKED_15M", StateLocked15M},
		{"GUARDIAN_APPROVED", StateGuardianApproved},
		{"VETOED", StateVetoed},
		{"EXPIRED", StateExpired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.state) != tt.name {
				t.Errorf("state = %q, want %q", tt.state, tt.name)
			}
		})
	}
}

// TestVaultRecordAttributeNames pins the persisted attribute names, in field
// order. Adding, renaming or removing a VaultRecord field changes what is
// stored, so it has to be a deliberate change here too. The list also shows
// that no field holds a raw identifier, an OTP, a PIN or a password
// (Requirements 12.1, 12.2).
func TestVaultRecordAttributeNames(t *testing.T) {
	want := []string{
		"id",
		"state",
		"risk_score",
		"payer_account_masked",
		"beneficiary_masked",
		"upi_masked",
		"phone_masked",
		"amount_inr",
		"created_at",
		"version",
		"expires_at",
		"lock_deadline",
		"token_expiry",
		"settled",
		"reason",
		"evidence",
		"explanation",
	}

	typ := reflect.TypeOf(VaultRecord{})
	got := make([]string, 0, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("dynamodbav")
		got = append(got, strings.Split(tag, ",")[0])
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("attribute names = %v, want %v", got, want)
	}
}

// TestVaultRecordLockFieldsAreEpochSeconds checks the types of the vault
// fields. LockDeadline and TokenExpiry are int64 epoch seconds, not time.Time,
// so a zero value ("never locked") round-trips through DynamoDB unchanged.
func TestVaultRecordLockFieldsAreEpochSeconds(t *testing.T) {
	typ := reflect.TypeOf(VaultRecord{})
	tests := []struct {
		field string
		want  reflect.Kind
	}{
		{"LockDeadline", reflect.Int64},
		{"TokenExpiry", reflect.Int64},
		{"Settled", reflect.Bool},
		{"Reason", reflect.String},
		{"EvidenceSummary", reflect.String},
	}
	for _, tt := range tests {
		f, ok := typ.FieldByName(tt.field)
		if !ok {
			t.Errorf("VaultRecord has no field %s", tt.field)
			continue
		}
		if f.Type.Kind() != tt.want {
			t.Errorf("%s has kind %v, want %v", tt.field, f.Type.Kind(), tt.want)
		}
	}
	if _, ok := typ.FieldByName("LockExpiresAt"); ok {
		t.Error("VaultRecord still has LockExpiresAt, which LockDeadline replaces")
	}
}
