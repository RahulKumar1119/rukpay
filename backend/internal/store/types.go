package store

import "time"

// VaultState names a state of the cooling-off vault. The store persists any
// VaultState as given; validating transitions between states belongs to the
// later vault package (Requirement 6.3).
type VaultState string

// Vault states. Each constant holds its own name as its string value
// (Requirement 6.1).
const (
	StateInstantPass      VaultState = "INSTANT_PASS"
	StateRealityCheck     VaultState = "REALITY_CHECK"
	StateLocked15M        VaultState = "LOCKED_15M"
	StateGuardianApproved VaultState = "GUARDIAN_APPROVED"
	StateVetoed           VaultState = "VETOED"
	StateExpired          VaultState = "EXPIRED"
)

// VaultRecord is the persisted form of one vault. It has masked identifiers
// only. There is no field for raw identifiers, OTPs, PINs or passwords
// (Requirements 12.1, 12.2).
//
// Version is owned by the caller. The store writes a record as given and never
// increments Version. ExpiresAt is the DynamoDB TTL attribute, stored as Unix
// epoch seconds.
type VaultRecord struct {
	ID                 string     `dynamodbav:"id"`
	State              VaultState `dynamodbav:"state"`
	RiskScore          int        `dynamodbav:"risk_score"`
	PayerAccountMasked string     `dynamodbav:"payer_account_masked,omitempty"`
	BeneficiaryMasked  string     `dynamodbav:"beneficiary_masked,omitempty"`
	UPIMasked          string     `dynamodbav:"upi_masked,omitempty"`
	PhoneMasked        string     `dynamodbav:"phone_masked,omitempty"`
	AmountINR          int64      `dynamodbav:"amount_inr"`
	CreatedAt          time.Time  `dynamodbav:"created_at,unixtime"`
	LockExpiresAt      time.Time  `dynamodbav:"lock_expires_at,unixtime"`
	Version            int        `dynamodbav:"version"`
	ExpiresAt          time.Time  `dynamodbav:"expires_at,unixtime"`
}
