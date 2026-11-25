package store

import (
	"context"
	"math"
	"time"
)

// TrustLevel says whether the payer has chosen to trust a saved recipient.
type TrustLevel string

// Trust levels. A recipient starts at TrustNew.
const (
	TrustNew     TrustLevel = "NEW"
	TrustTrusted TrustLevel = "TRUSTED"
)

// ProtectionLevel says how much care RukPay takes with payments to a saved
// recipient.
type ProtectionLevel string

// Protection levels. A recipient starts at ProtectionExtraCare.
const (
	ProtectionExtraCare ProtectionLevel = "EXTRA_CARE"
	ProtectionStandard  ProtectionLevel = "STANDARD"
)

// HistoryEntry is one line of a recipient's own history: when something
// happened and which of nine codes it was. AmountINR is set for the PAYMENT
// code only. The store gives Kind no meaning and keeps it as given. An entry
// holds no label, identifier, id or free text (Requirement 19.7). All fields are
// plain values, so a struct copy is a deep copy.
type HistoryEntry struct {
	At        int64  `dynamodbav:"at" json:"at"`
	Kind      string `dynamodbav:"kind" json:"kind"`
	AmountINR int64  `dynamodbav:"amount_inr,omitempty" json:"amount_inr,omitempty"`
}

// BeneficiaryTTLSeconds is how long a Beneficiary_Record lives after its latest
// write: 30 days, in seconds (Requirement 20.4).
const BeneficiaryTTLSeconds = 2_592_000

// BeneficiaryRecord is the persisted form of one saved recipient, with its
// history. The only identifier it holds is IdentifierMasked, which a store
// accepts only in a masked format. There is no field for a raw identifier
// (Requirements 14.4, 24.1).
//
// The store owns two fields. ExpiresAt, the DynamoDB TTL attribute in epoch
// seconds, is set on every create and replace to the time of that write plus
// BeneficiaryTTLSeconds, whatever value the caller sends. Version is set by a
// replace to the expected version plus 1, whatever value the caller sends. A
// create keeps the Version it is given. The store keeps every other field as
// given and gives it no meaning.
//
// VerifiedAt and FirstPaymentINR are pointers so that "absent" survives every
// round trip and is never read back as 0. History keeps nil and empty distinct,
// the way VaultRecord.Explanation does. The record holds a slice and two
// pointers, so it is not comparable with == and a plain struct copy shares them.
// Compare records with reflect.DeepEqual and use clone for an independent copy.
type BeneficiaryRecord struct {
	ID               string          `dynamodbav:"beneficiary_id"`
	Label            string          `dynamodbav:"label"`
	IdentifierMasked string          `dynamodbav:"identifier_masked"`
	AddedAt          int64           `dynamodbav:"added_at"`
	VerifiedAt       *int64          `dynamodbav:"verified_at,omitempty"`
	Trust            TrustLevel      `dynamodbav:"trust_level"`
	Protection       ProtectionLevel `dynamodbav:"protection_level"`
	PressureFlag     bool            `dynamodbav:"pressure_flag"`
	Version          int             `dynamodbav:"version"`
	PaymentCount     int             `dynamodbav:"payment_count"`
	FirstPaymentINR  *int64          `dynamodbav:"first_payment_amount,omitempty"`
	History          []HistoryEntry  `dynamodbav:"history,omitempty"` // appended order, the service keeps at most 50
	ExpiresAt        int64           `dynamodbav:"expires_at"`
}

// clone returns a copy of rec that shares no memory with it: the history slice
// and both pointed-to values are copied. A nil History stays nil, an empty one
// stays empty, and a nil pointer stays nil, so presence survives the copy.
func (rec BeneficiaryRecord) clone() BeneficiaryRecord {
	if rec.VerifiedAt != nil {
		v := *rec.VerifiedAt
		rec.VerifiedAt = &v
	}
	if rec.FirstPaymentINR != nil {
		v := *rec.FirstPaymentINR
		rec.FirstPaymentINR = &v
	}
	if rec.History != nil {
		rec.History = append([]HistoryEntry{}, rec.History...)
	}
	return rec
}

// BeneficiaryStore persists the saved recipients of a payer. It is separate from
// VaultStore and HistoryStore, which keep their methods. MemoryStore and
// DynamoDBStore implement all three.
//
// The store does persistence only. It checks no trust rule, no label rule and no
// limit on the number of records or entries: those belong to the beneficiary
// package.
//
// Every method honors ctx and returns ctx.Err() when the context is already
// cancelled, before any work and with no client request. An error text holds the
// operation, the Beneficiary_Id (or "-" for a list) and a fixed description or
// sentinel text, and never a label, an identifier, an amount or an entry
// (Requirements 20.7, 24.5). Callers match the kind with errors.Is.
//
// A record is expired when its ExpiresAt is earlier than the store's clock in
// whole seconds, and an ExpiresAt equal to the clock is not expired. An expired
// record is hidden from get and list, refused by replace and delete, and still
// blocks a create, because DynamoDB keeps an expired item until its TTL deletion
// runs (Requirements 20.4, 20.10).
type BeneficiaryStore interface {
	// CreateBeneficiary stores rec under payer as a new record. It returns
	// ErrUnmaskedIdentifier, before any other check, when rec.IdentifierMasked is
	// empty or not a masked identifier, and ErrAlreadyExists when rec.ID is
	// stored, expired or not. The stored ExpiresAt is the time of the write plus
	// BeneficiaryTTLSeconds. No error writes anything (Requirements 20.4, 20.6,
	// 20.10).
	CreateBeneficiary(ctx context.Context, payer string, rec BeneficiaryRecord) error

	// GetBeneficiary returns a copy of the record stored under (payer, id). It
	// returns ErrNotFound when none is stored or it has expired (Requirement
	// 20.11).
	GetBeneficiary(ctx context.Context, payer, id string) (BeneficiaryRecord, error)

	// ListBeneficiaries returns copies of the unexpired records of payer in
	// ascending id order, and an empty non-nil list when there are none. It
	// returns no record of another payer and no other item. DynamoDBStore serves
	// it with one Query per page on the partition of payer, following
	// LastEvaluatedKey, and sends no Scan and names no index (Requirement 20.2).
	ListBeneficiaries(ctx context.Context, payer string) ([]BeneficiaryRecord, error)

	// ReplaceBeneficiary replaces the record stored under (payer, id) with next
	// in one atomic operation, but only when the stored Version equals
	// expectedVersion. It stores next with Version expectedVersion + 1 and a new
	// ExpiresAt, and keys the item by id, not next.ID, as UpdateIf does. It
	// returns ErrUnmaskedIdentifier, before any other check, when
	// next.IdentifierMasked is empty or not masked, ErrNotFound when none is
	// stored or it has expired, and ErrConditionFailed when the stored Version
	// differs. Every error leaves the stored record unchanged (Requirements
	// 20.3, 20.6, 20.9, 20.11).
	ReplaceBeneficiary(ctx context.Context, payer, id string, expectedVersion int, next BeneficiaryRecord) error

	// DeleteBeneficiary removes the record stored under (payer, id) with its
	// history, but only when the stored Version equals expectedVersion, so that a
	// change and a delete that read one version cannot both apply (Requirement
	// 15.10). It returns ErrNotFound when none is stored or it has expired, and
	// ErrConditionFailed when the stored Version differs. Every error leaves the
	// stored record unchanged (Requirement 20.12).
	DeleteBeneficiary(ctx context.Context, payer, id string, expectedVersion int) error
}

// Operation names used in beneficiary error texts. opNoID, from history.go,
// stands in for the id of a list.
const (
	opCreateBeneficiary  = "create_beneficiary"
	opGetBeneficiary     = "get_beneficiary"
	opListBeneficiaries  = "list_beneficiaries"
	opReplaceBeneficiary = "replace_beneficiary"
	opDeleteBeneficiary  = "delete_beneficiary"
)

// skBeneficiaryPrefix starts the sort key of every Beneficiary_Record item. The
// sort key holds the Beneficiary_Id after it.
const skBeneficiaryPrefix = "B#"

// beneficiarySortKey returns the sort key of the record with Beneficiary_Id id.
// Sorting these keys as strings sorts the records by id, byte-wise, because the
// prefix is the same for all of them.
func beneficiarySortKey(id string) string { return skBeneficiaryPrefix + id }

// beneficiaryExpiry returns the ExpiresAt of a record written at now: the time of
// the write plus BeneficiaryTTLSeconds, saturating at the largest int64 rather
// than wrapping.
func beneficiaryExpiry(now time.Time) int64 {
	at := now.Unix()
	if at > math.MaxInt64-BeneficiaryTTLSeconds {
		return math.MaxInt64
	}
	return at + BeneficiaryTTLSeconds
}

// beneficiaryExpired reports whether rec has passed its ExpiresAt at time now. The
// comparison is in whole seconds, so ExpiresAt equal to now is not expired. An
// ExpiresAt of 0 is expired: the store always sets it, so only an item that
// someone else wrote lacks it, and DynamoDB's replace condition treats a missing
// attribute the same way.
func beneficiaryExpired(rec BeneficiaryRecord, now time.Time) bool {
	return rec.ExpiresAt < now.Unix()
}
