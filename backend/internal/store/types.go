package store

import "time"

// VaultState names a state of the cooling-off vault. The store persists any
// VaultState as given; validating transitions between states belongs to the
// vault package (Requirement 6.3).
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
//
// The last five fields before Explanation belong to the cooling-off vault. The
// store keeps them as given and gives them no meaning. LockDeadline and
// TokenExpiry are int64 epoch seconds, not time.Time, so that a zero value
// ("never locked") round-trips through DynamoDB unchanged.
//
// Explanation holds a slice, so a VaultRecord is no longer comparable with ==
// and a plain struct copy shares the slice. Use clone for an independent copy.
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
	Version            int        `dynamodbav:"version"`
	ExpiresAt          time.Time  `dynamodbav:"expires_at,unixtime"`

	// LockDeadline is when the lock ends, in epoch seconds. It is 0 when the
	// vault was never locked.
	LockDeadline int64 `dynamodbav:"lock_deadline"`
	// TokenExpiry is the lock deadline as first set, in epoch seconds. A
	// demo fast-forward moves LockDeadline but never TokenExpiry.
	TokenExpiry int64 `dynamodbav:"token_expiry"`
	// Settled reports that the user has finished the step this state asks for.
	Settled bool `dynamodbav:"settled"`
	// Reason is a plain-language explanation shown to the user. It holds no
	// identifiers.
	Reason string `dynamodbav:"reason"`
	// EvidenceSummary is the masked report written when a vault is vetoed.
	EvidenceSummary string `dynamodbav:"evidence,omitempty"`
	// Explanation lists the signals that added points to RiskScore, written once
	// when the vault is created and never recomputed. A nil slice means the
	// record has no Explanation (it was written before the feature). A non-nil
	// empty slice means an Explanation that holds no Contribution. The store
	// keeps the distinction through every round trip (Requirements 2.3, 2.4).
	Explanation []Contribution `dynamodbav:"explanation,omitempty"`
}

// Contribution is one line of an Explanation: the signal that added points, how
// many, and one calm sentence saying why. Reason holds a fixed sentence and no
// identifier (Requirement 2.10). Points is negative only for the two cap lines.
// It is defined here, and not in the risk package, because risk imports store.
type Contribution struct {
	Signal string `json:"signal" dynamodbav:"signal"`
	Points int    `json:"points" dynamodbav:"points"`
	Reason string `json:"reason" dynamodbav:"reason"`
}

// clone returns a copy of rec that shares no memory with it. A nil Explanation
// stays nil and an empty one stays empty, so presence survives the copy.
func (rec VaultRecord) clone() VaultRecord {
	if rec.Explanation != nil {
		rec.Explanation = append([]Contribution{}, rec.Explanation...)
	}
	return rec
}
