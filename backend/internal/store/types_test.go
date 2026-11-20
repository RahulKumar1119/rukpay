package store

import "testing"

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
