package store

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"strings"
	"time"
)

// PayerKey names the one payer of the Phase 1 demo. Every History_Event is
// stored under it. It is a server constant, so the browser holds no identifier.
// A later account system replaces it with an account id.
const PayerKey = "demo-payer"

// HistoryRetentionSeconds is how long a History_Event stays within the
// History_Retention: 7 days, in seconds.
const HistoryRetentionSeconds = 604_800

// SnapshotLimit is the most History_Events that one evaluation reads.
const SnapshotLimit = 100

// Operation names used in history error texts, and the id that stands in for
// the missing event id of a read.
const (
	opAppendHistory = "append_history"
	opRecentHistory = "recent_history"
	opNoID          = "-"
)

// HistoryEvent is one payment the payer made, kept so that later evaluations can
// score behaviour. It holds a time, an amount, a masked recipient reference and
// the ids that locate it, and nothing else. There is no field for a raw
// identifier, an OTP, a PIN, a password, a label or a Beneficiary_Id
// (Requirements 12.6, 24.1).
//
// At is the evaluation time and ExpiresAt the TTL attribute, both in epoch
// seconds. ExpiresAt of 0 means the event never expires. The service sets
// ExpiresAt with HistoryExpiry. The stores keep the value they are given and
// never change it (Requirements 12.10, 13.8, 24.8). All fields are plain
// values, so a struct copy is a deep copy.
//
// BeneficiaryRef is empty or a masked identifier. A store refuses any other
// value with ErrUnmaskedIdentifier (Requirement 13.9).
type HistoryEvent struct {
	ID             string `dynamodbav:"event_id"`
	At             int64  `dynamodbav:"at"`
	AmountINR      int64  `dynamodbav:"amount_inr"`
	BeneficiaryRef string `dynamodbav:"beneficiary_ref,omitempty"`
	ExpiresAt      int64  `dynamodbav:"expires_at"`
}

// HistoryExpiry returns the ExpiresAt of a History_Event with time at: at plus
// HistoryRetentionSeconds. It saturates at the largest int64 instead of
// wrapping, so a very late event time never gets an expiry in the past.
func HistoryExpiry(at int64) int64 {
	if at > math.MaxInt64-HistoryRetentionSeconds {
		return math.MaxInt64
	}
	return at + HistoryRetentionSeconds
}

// HistoryStore persists History_Events. It is separate from VaultStore, which
// keeps exactly its three methods. MemoryStore and DynamoDBStore implement
// both.
//
// Every method honors ctx and returns ctx.Err() when the context is already
// cancelled, before any work and with no client request (Requirement 13.13).
// An error text holds the operation, the event id and a fixed description or
// sentinel text, and never an amount, a time or a reference (Requirement 24.5).
type HistoryStore interface {
	// AppendHistory writes ev under payer as a new item. It returns
	// ErrUnmaskedIdentifier, before anything else, when ev.BeneficiaryRef is
	// not empty and not a masked identifier, and ErrAlreadyExists when an event
	// with the same payer, time and id is stored, which stays unchanged. No
	// error writes anything. Events with different ids never overwrite one
	// another, even at equal times (Requirements 13.4, 13.9, 13.12).
	AppendHistory(ctx context.Context, payer string, ev HistoryEvent) error

	// RecentHistory examines the limit events of payer with the greatest
	// (At, ID), where IDs compare byte-wise, drops the ones whose ExpiresAt is
	// not 0 and earlier than the store's clock in whole seconds, and returns the
	// rest in ascending (At, ID) order. An ExpiresAt equal to the current time
	// is not expired (Requirement 13.7). It returns an empty non-nil list and
	// no error when payer has no such events or limit is below 1, and sends no
	// request for a limit below 1 (Requirement 13.11).
	//
	// Dropping the expired events after the limit is applied is deliberate: one
	// DynamoDB Query with Limit counts expired items too and cannot skip them.
	// Events that the service writes expire in the order of their times, so the
	// dropped events are the oldest and the result is also the newest limit
	// unexpired events. limit must fit one response page (1 MB, about 5,000
	// events). SnapshotLimit is 100.
	RecentHistory(ctx context.Context, payer string, limit int) ([]HistoryEvent, error)
}

// skHistoryPrefix starts the sort key of every History_Event item.
const skHistoryPrefix = "E#"

// historySortKey returns the sort key of the History_Event with time at and id.
// Sorting the keys as strings sorts the events by time and then by id, for
// every int64 time: the time is written as 20 decimal digits after flipping the
// sign bit, which maps int64 order onto uint64 order, so negative times sort
// before positive ones. The id comes last, so any characters in it keep the
// byte-wise id order.
func historySortKey(at int64, id string) string {
	return fmt.Sprintf("%s%020d#%s", skHistoryPrefix, uint64(at)^(1<<63), id)
}

// historyExpired reports whether ev has passed its ExpiresAt at time now. The
// comparison is in whole seconds, so ExpiresAt equal to now is not expired and
// an ExpiresAt of 0 never expires. It does not convert ExpiresAt to a
// time.Time, which could not hold a value near the largest int64.
func historyExpired(ev HistoryEvent, now time.Time) bool {
	return ev.ExpiresAt != 0 && ev.ExpiresAt < now.Unix()
}

// validateHistoryEvent returns ErrUnmaskedIdentifier when ev.BeneficiaryRef is
// not empty and not a masked identifier. It checks no other field.
func validateHistoryEvent(ev HistoryEvent) error {
	if ev.BeneficiaryRef == "" {
		return nil
	}
	return ValidateMaskedIdentifier(ev.BeneficiaryRef)
}

// compareHistory orders events by time and then by byte-wise id.
func compareHistory(a, b HistoryEvent) int {
	if c := cmp.Compare(a.At, b.At); c != 0 {
		return c
	}
	return strings.Compare(a.ID, b.ID)
}
