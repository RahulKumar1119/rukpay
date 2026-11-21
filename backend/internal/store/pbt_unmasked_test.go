package store

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"pgregory.net/rapid"
)

// unmaskedField describes one identifier field of VaultRecord: how to set it
// and the mask format it must match.
type unmaskedField struct {
	name string
	re   *regexp.Regexp
	set  func(rec *VaultRecord, v string)
	raw  *rapid.Generator[string] // plausible raw values of the field
}

func unmaskedFields() []unmaskedField {
	return []unmaskedField{
		{
			name: "payer_account_masked",
			re:   reAccount,
			set:  func(rec *VaultRecord, v string) { rec.PayerAccountMasked = v },
			raw:  rapid.StringMatching(`^[0-9]{9,18}$`),
		},
		{
			name: "beneficiary_masked",
			re:   reAccount,
			set:  func(rec *VaultRecord, v string) { rec.BeneficiaryMasked = v },
			raw:  rapid.StringMatching(`^[0-9]{9,18}$`),
		},
		{
			name: "upi_masked",
			re:   reUPI,
			set:  func(rec *VaultRecord, v string) { rec.UPIMasked = v },
			raw:  rapid.StringMatching(`^[a-z]{3,10}@[a-z]{3,8}$`),
		},
		{
			name: "phone_masked",
			re:   rePhone,
			set:  func(rec *VaultRecord, v string) { rec.PhoneMasked = v },
			raw:  rapid.StringMatching(`^(\+91)?[6-9][0-9]{9}$`),
		},
	}
}

// unmaskedBadValueGen draws a non-empty value that does not match the mask
// format of f: either a plausible raw identifier or any other string.
func unmaskedBadValueGen(f unmaskedField) *rapid.Generator[string] {
	return rapid.OneOf(
		f.raw,
		rapid.String().Filter(func(s string) bool { return s != "" && !f.re.MatchString(s) }),
	)
}

// unmaskedBadRecordGen draws a valid record and replaces one identifier field
// with a value that is not masked.
func unmaskedBadRecordGen(base VaultRecord) *rapid.Generator[VaultRecord] {
	return rapid.Custom(func(t *rapid.T) VaultRecord {
		fields := unmaskedFields()
		f := fields[rapid.IntRange(0, len(fields)-1).Draw(t, "field")]
		bad := base
		f.set(&bad, unmaskedBadValueGen(f).Draw(t, f.name))
		return bad
	})
}

// Feature: behavioral-risk-engine, Property 14: Unmasked identifiers are never persisted
//
// A record with a non-empty identifier that does not match its mask format is
// rejected by Create and by UpdateIf, and the store keeps what it held before.
// A record whose identifiers are empty or masked is not rejected for that
// reason.
//
// **Validates: Requirements 12.3**
func TestUnmaskedIdentifiersAreNeverPersisted(t *testing.T) {
	ctx := context.Background()

	contractCheck(t, func(rt *rapid.T, env *contractEnv) {
		good := contractRecordGen().Draw(rt, "good")
		bad := unmaskedBadRecordGen(good).Draw(rt, "bad")

		// Create rejects the bad record and stores nothing.
		err := env.vs.Create(ctx, bad)
		if !errors.Is(err, ErrUnmaskedIdentifier) {
			rt.Fatalf("Create(%+v) = %v, want ErrUnmaskedIdentifier", bad, err)
		}
		if _, err := env.vs.Get(ctx, bad.ID); !errors.Is(err, ErrNotFound) {
			rt.Fatalf("Get after rejected Create = %v, want ErrNotFound", err)
		}
		if env.fake != nil && (env.fake.itemCount() != 0 || env.fake.putCalls != 0) {
			rt.Fatalf("rejected Create reached the client: %d items, %d puts",
				env.fake.itemCount(), env.fake.putCalls)
		}

		// A valid record is accepted.
		contractMustCreate(rt, env.vs, good)

		// UpdateIf rejects the bad replacement and keeps the stored record.
		err = env.vs.UpdateIf(ctx, good.ID, good.State, bad)
		if !errors.Is(err, ErrUnmaskedIdentifier) {
			rt.Fatalf("UpdateIf(%+v) = %v, want ErrUnmaskedIdentifier", bad, err)
		}
		contractWantRecord(rt, env.vs, good.ID, good)

		// A valid replacement with the same state is accepted.
		if err := env.vs.UpdateIf(ctx, good.ID, good.State, good); err != nil {
			rt.Fatalf("UpdateIf with a valid record = %v, want nil", err)
		}
	})
}
