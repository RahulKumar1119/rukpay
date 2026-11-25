package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"pgregory.net/rapid"
)

// Property 15 is checked by the tests of this file. Each runs against MemoryStore
// and against DynamoDBStore over the fake through contractCheck (or
// contractCheckDynamo for the item layout and the requests), so both adapters
// face the same generated inputs. The model below is written separately from the
// stores: it uses plain maps and comparisons and shares no code with them, not
// even the deep copy.

// beneficiaryOf returns the BeneficiaryStore of env, or stops the test.
func beneficiaryOf(t rapid.TB, env *contractEnv) BeneficiaryStore {
	t.Helper()
	bs, ok := env.vs.(BeneficiaryStore)
	if !ok {
		t.Fatalf("%T does not implement BeneficiaryStore", env.vs)
	}
	return bs
}

// benePayers are the payer keys the generators use. They include keys that look
// like the other key families and keys that are prefixes of one another, so a
// collision between partitions would show.
var benePayers = []string{PayerKey, "other-payer", "", "VAULT#x", "HIST#y", "BENE#z", "a#b", "a"}

func benePayerGen() *rapid.Generator[string] { return rapid.SampledFrom(benePayers) }

// beneIDs is a small pool, so records share ids and the same id is used under
// several payers. It holds ids that sort in a way that tests byte order: a
// prefix of another id, a multi-byte character and the empty id.
var beneIDs = []string{"a", "b", "ab", "B#a", "#", "é", "", "0123456789abcdef0123456789abcdef", "ffffffffffffffffffffffffffffffff"}

func beneIDGen() *rapid.Generator[string] { return rapid.SampledFrom(beneIDs) }

// beneKinds are the nine History_Entry codes of the service, and two strings that
// are no code. The store keeps Kind as given and gives it no meaning.
var beneKinds = []string{
	"ADDED", "VERIFIED", "VERIFICATION_NOT_PASSED", "TRUSTED", "TRUST_REMOVED",
	"PROTECTION_RAISED", "PROTECTION_LOWERED", "RENAMED", "PAYMENT", "", "anything else",
}

// beneTimeGen draws a time in epoch seconds: the extremes of int64, a window
// around the contract clock and any int64.
func beneTimeGen() *rapid.Generator[int64] {
	now := contractEpoch().Unix()
	return rapid.OneOf(
		rapid.SampledFrom([]int64{math.MinInt64, -1, 0, 1, math.MaxInt64}),
		rapid.Int64Range(now-1_000_000, now+1_000_000),
		rapid.Int64(),
	)
}

func beneAmountGen() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{0, 1, 450, 85000, math.MaxInt64, math.MinInt64, -1}),
		rapid.Int64(),
	)
}

func beneLabelGen() *rapid.Generator[string] {
	return rapid.OneOf(
		rapid.Just(""),
		rapid.StringMatching(`^[ -~]{1,40}$`),
		rapid.String(),
	)
}

// beneValidIdentifierGen draws a masked identifier of any of the three formats.
func beneValidIdentifierGen() *rapid.Generator[string] {
	return rapid.OneOf(contractAccountGen(), contractPhoneGen(), contractUPIGen())
}

// beneUnmaskedGen draws an identifier that is not masked: the empty string, raw
// numbers and ids, near misses of each mask, and any printable text that matches
// no mask.
func beneUnmaskedGen() *rapid.Generator[string] {
	return rapid.OneOf(
		rapid.Just(""),
		rapid.StringMatching(`^[0-9]{9,18}$`),
		rapid.StringMatching(`^\+91[0-9]{10}$`),
		rapid.StringMatching(`^[a-z]{3,8}@[a-z]{2,8}$`),
		rapid.SampledFrom([]string{
			"XXXX-XXXX-482", "XXXX-XXXX-48211", " XXXX-XXXX-4821", "XXXX-XXXX-4821 ", "XXXX-XXXX-4821\n",
			"xxxx-xxxx-4821", "+91-XXXXXX321", "+91-XXXXX03210", "r**@okaxis", "r***@", "***@okaxis",
			"r***@ok axis", "ab***@okaxis",
		}),
		rapid.StringMatching(`^[ -~]{1,30}$`).Filter(func(s string) bool { return MaskedFormat(s) == MaskNone }),
	)
}

func beneEntryGen() *rapid.Generator[HistoryEntry] {
	return rapid.Custom(func(t *rapid.T) HistoryEntry {
		return HistoryEntry{
			At:        beneTimeGen().Draw(t, "at"),
			Kind:      rapid.SampledFrom(beneKinds).Draw(t, "kind"),
			AmountINR: rapid.OneOf(rapid.Just(int64(0)), beneAmountGen()).Draw(t, "amount_inr"),
		}
	})
}

// beneHistoryGen draws a nil history, an empty one and one of 1 to 50 entries. A
// slice of entries is fresh for each draw, so a test may change it.
func beneHistoryGen() *rapid.Generator[[]HistoryEntry] {
	return rapid.OneOf(
		rapid.Just[[]HistoryEntry](nil),
		rapid.Just([]HistoryEntry{}), // one shared empty slice: it has no element to change
		rapid.SliceOfN(beneEntryGen(), 1, 50),
	)
}

// beneRecordGen draws a valid BeneficiaryRecord with a masked identifier. Its
// ExpiresAt is any int64, because the store must ignore what the caller sends.
// Version is kept below 1,001 so that a replace has room to add 1.
func beneRecordGen() *rapid.Generator[BeneficiaryRecord] {
	return rapid.Custom(func(t *rapid.T) BeneficiaryRecord {
		return BeneficiaryRecord{
			ID:               beneIDGen().Draw(t, "id"),
			Label:            beneLabelGen().Draw(t, "label"),
			IdentifierMasked: beneValidIdentifierGen().Draw(t, "identifier_masked"),
			AddedAt:          beneTimeGen().Draw(t, "added_at"),
			VerifiedAt:       rapid.Ptr(beneTimeGen(), true).Draw(t, "verified_at"),
			Trust:            rapid.SampledFrom([]TrustLevel{TrustNew, TrustTrusted}).Draw(t, "trust_level"),
			Protection:       rapid.SampledFrom([]ProtectionLevel{ProtectionExtraCare, ProtectionStandard}).Draw(t, "protection_level"),
			PressureFlag:     rapid.Bool().Draw(t, "pressure_flag"),
			Version:          rapid.IntRange(0, 1000).Draw(t, "version"),
			PaymentCount:     rapid.IntRange(0, 100_000).Draw(t, "payment_count"),
			FirstPaymentINR:  rapid.Ptr(beneAmountGen(), true).Draw(t, "first_payment_amount"),
			History:          beneHistoryGen().Draw(t, "history"),
			ExpiresAt:        beneTimeGen().Draw(t, "expires_at"),
		}
	})
}

// beneCopy returns a deep copy of rec. It is written here and not borrowed from
// the package, so that a broken clone in the package cannot hide in the model.
func beneCopy(rec BeneficiaryRecord) BeneficiaryRecord {
	out := rec
	if rec.VerifiedAt != nil {
		v := *rec.VerifiedAt
		out.VerifiedAt = &v
	}
	if rec.FirstPaymentINR != nil {
		v := *rec.FirstPaymentINR
		out.FirstPaymentINR = &v
	}
	if rec.History != nil {
		out.History = make([]HistoryEntry, len(rec.History))
		copy(out.History, rec.History)
	}
	return out
}

// beneTTL is the expected lifetime of a record after its latest write, written
// out as a number so that a change to the constant fails the tests.
const beneTTL = 2_592_000

// beneStored returns rec as the store must hold it after a write at time now: the
// ExpiresAt the caller sent is replaced by now plus 30 days.
func beneStored(rec BeneficiaryRecord, now int64) BeneficiaryRecord {
	out := beneCopy(rec)
	out.ExpiresAt = now + beneTTL
	return out
}

// beneErrorText is the exact text of a store error: the operation, the id and the
// text of the sentinel.
func beneErrorText(op, id string, kind error) string { return op + " " + id + ": " + kind.Error() }

// beneWantError stops the test unless err is nil when want is nil, and otherwise
// is of kind want with exactly the operation, the id and the sentinel text.
func beneWantError(t rapid.TB, what string, err, want error, op, id string) {
	t.Helper()
	if want == nil {
		if err != nil {
			t.Fatalf("%s: returned %v, want nil", what, err)
		}
		return
	}
	if !errors.Is(err, want) {
		t.Fatalf("%s: returned %v, want an error of kind %q", what, err, want)
	}
	if got, wantText := err.Error(), beneErrorText(op, id, want); got != wantText {
		t.Fatalf("%s: error text = %q, want %q", what, got, wantText)
	}
}

// beneMustCreate writes rec and stops the test on an error.
func beneMustCreate(t rapid.TB, bs BeneficiaryStore, payer string, rec BeneficiaryRecord) {
	t.Helper()
	if err := bs.CreateBeneficiary(context.Background(), payer, rec); err != nil {
		t.Fatalf("CreateBeneficiary(payer %q, id %q) = %v, want nil", payer, rec.ID, err)
	}
}

// beneWantGet stops the test unless GetBeneficiary returns want.
func beneWantGet(t rapid.TB, bs BeneficiaryStore, payer, id string, want BeneficiaryRecord) {
	t.Helper()
	got, err := bs.GetBeneficiary(context.Background(), payer, id)
	if err != nil {
		t.Fatalf("GetBeneficiary(payer %q, id %q) = %v, want nil", payer, id, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetBeneficiary(payer %q, id %q) =\n  %#v\nwant\n  %#v", payer, id, got, want)
	}
}

// beneWantList stops the test unless ListBeneficiaries returns want, as a non-nil
// list.
func beneWantList(t rapid.TB, bs BeneficiaryStore, payer string, want []BeneficiaryRecord) {
	t.Helper()
	got, err := bs.ListBeneficiaries(context.Background(), payer)
	if err != nil {
		t.Fatalf("ListBeneficiaries(payer %q) = %v, want nil", payer, err)
	}
	if got == nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("ListBeneficiaries(payer %q) =\n  %#v\nwant\n  %#v", payer, got, want)
	}
}

// beneModel is the reference: the records each payer holds, with the ExpiresAt the
// store must have set, and the time of the clock.
type beneModel struct {
	now   int64
	items map[string]map[string]BeneficiaryRecord
}

func newBeneModel(now int64) *beneModel {
	return &beneModel{now: now, items: make(map[string]map[string]BeneficiaryRecord)}
}

func (m *beneModel) expired(rec BeneficiaryRecord) bool { return rec.ExpiresAt < m.now }

// create applies a create and returns the error kind the store must return.
func (m *beneModel) create(payer string, rec BeneficiaryRecord, masked bool) error {
	if !masked {
		return ErrUnmaskedIdentifier
	}
	if _, ok := m.items[payer][rec.ID]; ok {
		return ErrAlreadyExists
	}
	if m.items[payer] == nil {
		m.items[payer] = make(map[string]BeneficiaryRecord)
	}
	m.items[payer][rec.ID] = beneStored(rec, m.now)
	return nil
}

// live returns the unexpired record under (payer, id).
func (m *beneModel) live(payer, id string) (BeneficiaryRecord, bool) {
	rec, ok := m.items[payer][id]
	if !ok || m.expired(rec) {
		return BeneficiaryRecord{}, false
	}
	return rec, true
}

func (m *beneModel) get(payer, id string) (BeneficiaryRecord, error) {
	rec, ok := m.live(payer, id)
	if !ok {
		return BeneficiaryRecord{}, ErrNotFound
	}
	return beneCopy(rec), nil
}

// list returns the unexpired records of payer by ascending id.
func (m *beneModel) list(payer string) []BeneficiaryRecord {
	ids := make([]string, 0, len(m.items[payer]))
	for id, rec := range m.items[payer] {
		if !m.expired(rec) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	out := make([]BeneficiaryRecord, 0, len(ids))
	for _, id := range ids {
		out = append(out, beneCopy(m.items[payer][id]))
	}
	return out
}

func (m *beneModel) replace(payer, id string, expected int, next BeneficiaryRecord, masked bool) error {
	if !masked {
		return ErrUnmaskedIdentifier
	}
	cur, ok := m.live(payer, id)
	if !ok {
		return ErrNotFound
	}
	if cur.Version != expected {
		return ErrConditionFailed
	}
	stored := beneStored(next, m.now)
	stored.Version = expected + 1
	m.items[payer][id] = stored
	return nil
}

func (m *beneModel) remove(payer, id string, expected int) error {
	cur, ok := m.live(payer, id)
	if !ok {
		return ErrNotFound
	}
	if cur.Version != expected {
		return ErrConditionFailed
	}
	delete(m.items[payer], id)
	return nil
}

// Feature: risk-intelligence, Property 15: The beneficiary store contract
//
// For any sequence of up to 30 creates, gets, lists, replaces, deletes and clock
// moves over several payers and ids, including repeats of an id, versions that are
// right, one above and one below, identifiers that are not masked, and clock
// moves to the second before, at and after the expiry of a stored record: every
// result, its error kind and its exact error text equal the model's, a failed
// call changes nothing, and after each step the payer's list equals the model's.
// The records come back equal field for field to what was written (an absent
// verified time, an absent first payment and a nil or empty history included),
// with ExpiresAt equal to the write time plus 2,592,000 s whatever the caller
// sent, and with Version equal to the expected version plus 1 after a replace.
// An expired record is hidden from get and list, refused by replace and delete,
// and still blocks a create of its id.
//
// **Validates: Requirements 20.2, 20.3, 20.4, 20.5, 20.6, 20.7, 20.10, 20.11, 20.12**
func TestBeneficiaryContractMatchesTheModel(t *testing.T) {
	contractCheck(t, func(rt *rapid.T, env *contractEnv) {
		ctx := context.Background()
		bs := beneficiaryOf(rt, env)
		m := newBeneModel(env.clock.Now().Unix())

		maskedGen := rapid.SampledFrom([]bool{true, true, true, true, false})
		ops := []string{"create", "create", "create", "get", "get", "list", "replace", "replace", "replace", "delete", "delete", "clock", "clock"}

		steps := rapid.IntRange(1, 30).Draw(rt, "steps")
		for i := 0; i < steps; i++ {
			op := rapid.SampledFrom(ops).Draw(rt, "op")
			payer := benePayerGen().Draw(rt, "payer")
			touched := payer
			what := fmt.Sprintf("step %d (%s, payer %q)", i, op, payer)

			switch op {
			case "create":
				rec := beneRecordGen().Draw(rt, "record")
				masked := maskedGen.Draw(rt, "masked")
				if !masked {
					rec.IdentifierMasked = beneUnmaskedGen().Draw(rt, "unmasked")
				}
				err := bs.CreateBeneficiary(ctx, payer, rec)
				beneWantError(rt, what, err, m.create(payer, rec, masked), opCreateBeneficiary, rec.ID)

			case "get":
				id := beneIDGen().Draw(rt, "id")
				got, err := bs.GetBeneficiary(ctx, payer, id)
				want, wantErr := m.get(payer, id)
				beneWantError(rt, what, err, wantErr, opGetBeneficiary, id)
				if wantErr == nil && !reflect.DeepEqual(got, want) {
					rt.Fatalf("%s: got\n  %#v\nwant\n  %#v", what, got, want)
				}
				if wantErr != nil && !reflect.DeepEqual(got, BeneficiaryRecord{}) {
					rt.Fatalf("%s: returned a record %#v with an error", what, got)
				}

			case "list":
				// Checked below like after every step.

			case "replace", "delete":
				id := beneIDGen().Draw(rt, "id")
				choices := []int{0, 1, 2, 3}
				if cur, ok := m.items[payer][id]; ok {
					choices = append(choices, cur.Version, cur.Version, cur.Version, cur.Version+1, cur.Version-1)
				}
				expected := rapid.SampledFrom(choices).Draw(rt, "expected_version")
				if op == "delete" {
					err := bs.DeleteBeneficiary(ctx, payer, id, expected)
					beneWantError(rt, what, err, m.remove(payer, id, expected), opDeleteBeneficiary, id)
					break
				}
				next := beneRecordGen().Draw(rt, "next")
				if rapid.SampledFrom([]bool{true, true, true, false}).Draw(rt, "same_id") {
					next.ID = id // otherwise the record keeps another id inside its key
				}
				masked := maskedGen.Draw(rt, "masked")
				if !masked {
					next.IdentifierMasked = beneUnmaskedGen().Draw(rt, "unmasked")
				}
				err := bs.ReplaceBeneficiary(ctx, payer, id, expected, next)
				beneWantError(rt, what, err, m.replace(payer, id, expected, next, masked), opReplaceBeneficiary, id)

			case "clock":
				target := m.now + rapid.Int64Range(0, 5).Draw(rt, "step_s")
				if rapid.Bool().Draw(rt, "to_an_expiry") {
					var stored []BeneficiaryRecord
					for _, byID := range m.items {
						for _, rec := range byID {
							stored = append(stored, rec)
						}
					}
					if len(stored) > 0 {
						sort.Slice(stored, func(a, b int) bool { return stored[a].ExpiresAt < stored[b].ExpiresAt })
						pick := rapid.IntRange(0, len(stored)-1).Draw(rt, "which")
						target = stored[pick].ExpiresAt + rapid.Int64Range(-1, 1).Draw(rt, "around_expiry")
					}
				}
				env.clock.Set(time.Unix(target, 0).UTC())
				m.now = target
				touched = ""
			}

			if touched != "" {
				beneWantList(rt, bs, touched, m.list(touched))
			}
		}

		// At the end every payer and every id agrees with the model.
		for _, payer := range benePayers {
			beneWantList(rt, bs, payer, m.list(payer))
			for _, id := range beneIDs {
				got, err := bs.GetBeneficiary(ctx, payer, id)
				want, wantErr := m.get(payer, id)
				beneWantError(rt, "final get", err, wantErr, opGetBeneficiary, id)
				if wantErr == nil && !reflect.DeepEqual(got, want) {
					rt.Fatalf("final get of payer %q id %q: got\n  %#v\nwant\n  %#v", payer, id, got, want)
				}
			}
		}
	})
}

// Feature: risk-intelligence, Property 15: The beneficiary store contract
//
// For any identifier that is not in a masked format (empty, raw, a near miss),
// create and replace return ErrUnmaskedIdentifier before any other check: with an
// id that is free, stored, expired or unknown, with the right version and a wrong
// one, and with an expired record. Nothing is written, so the stored records
// stay as they were, DynamoDBStore sends no request at all, and the error text
// holds the operation and the id and never the identifier.
//
// **Validates: Requirements 20.6, 20.7**
func TestBeneficiaryUnmaskedIdentifierIsRefusedFirst(t *testing.T) {
	contractCheck(t, func(rt *rapid.T, env *contractEnv) {
		ctx := context.Background()
		bs := beneficiaryOf(rt, env)
		payer := benePayerGen().Draw(rt, "payer")
		stored := beneRecordGen().Draw(rt, "stored")
		stored.Version = rapid.IntRange(0, 5).Draw(rt, "version")
		beneMustCreate(rt, bs, payer, stored)
		want := beneStored(stored, env.clock.Now().Unix())

		// The record may be live or expired when the refused calls run.
		if rapid.Bool().Draw(rt, "expire") {
			env.clock.Set(time.Unix(want.ExpiresAt+1, 0).UTC())
		}
		wantList := []BeneficiaryRecord{}
		if env.clock.Now().Unix() <= want.ExpiresAt {
			wantList = []BeneficiaryRecord{beneCopy(want)}
		}
		callsBefore := historyCallsOf(env)
		var deletesBefore, itemsBefore int
		if env.fake != nil {
			deletesBefore, itemsBefore = env.fake.deleteCalls, env.fake.itemCount()
		}

		// attempt tries every refused write with one identifier. A digit-only
		// identifier cannot appear in the fixed error text, so only that kind is
		// checked for a leak.
		attempt := func(unmasked string, checkLeak bool) {
			rec := beneRecordGen().Draw(rt, "attempt")
			rec.IdentifierMasked = unmasked
			for _, id := range []string{stored.ID, "free-id", rec.ID} {
				rec.ID = id
				err := bs.CreateBeneficiary(ctx, payer, rec)
				beneWantError(rt, "create", err, ErrUnmaskedIdentifier, opCreateBeneficiary, id)
				if checkLeak && strings.Contains(err.Error(), unmasked) {
					rt.Fatalf("create error %q holds the identifier %q", err, unmasked)
				}
				for _, version := range []int{stored.Version, stored.Version + 1, stored.Version - 1} {
					err := bs.ReplaceBeneficiary(ctx, payer, id, version, rec)
					beneWantError(rt, "replace", err, ErrUnmaskedIdentifier, opReplaceBeneficiary, id)
					if checkLeak && strings.Contains(err.Error(), unmasked) {
						rt.Fatalf("replace error %q holds the identifier %q", err, unmasked)
					}
				}
			}
		}
		attempt(beneUnmaskedGen().Draw(rt, "unmasked"), false)
		attempt(rapid.StringMatching(`^[0-9]{9,18}$`).Draw(rt, "raw_number"), true)

		if got := historyCallsOf(env); got != callsBefore {
			rt.Fatalf("client calls = %+v after refused writes, want %+v (no request at all)", got, callsBefore)
		}
		if env.fake != nil && (env.fake.deleteCalls != deletesBefore || env.fake.itemCount() != itemsBefore) {
			rt.Fatalf("deleteCalls = %d, itemCount = %d after refused writes, want %d and %d",
				env.fake.deleteCalls, env.fake.itemCount(), deletesBefore, itemsBefore)
		}
		beneWantList(rt, bs, payer, wantList)
		if len(wantList) == 1 {
			beneWantGet(rt, bs, payer, stored.ID, want)
		}
	})
}

// Feature: risk-intelligence, Property 15: The beneficiary store contract
//
// For any record, a change to the record that the caller passed in, to what a get
// returned or to what a list returned never reaches the store: the history slice
// and both pointed-to values are copies in both directions, and a nil history,
// an empty one and an absent verified time or first payment stay what they were.
//
// **Validates: Requirements 20.5**
func TestBeneficiaryStoreSharesNoMemoryWithCallers(t *testing.T) {
	// vandalize changes every part of rec that a caller can reach.
	vandalize := func(rec *BeneficiaryRecord) {
		for i := range rec.History {
			rec.History[i] = HistoryEntry{At: -7, Kind: "vandal", AmountINR: -7}
		}
		if rec.History != nil {
			rec.History = append(rec.History[:0], HistoryEntry{At: -8, Kind: "vandal"})
		}
		if rec.VerifiedAt != nil {
			*rec.VerifiedAt = -9
		}
		if rec.FirstPaymentINR != nil {
			*rec.FirstPaymentINR = -9
		}
		rec.Label = "vandal"
	}

	contractCheck(t, func(rt *rapid.T, env *contractEnv) {
		ctx := context.Background()
		bs := beneficiaryOf(rt, env)
		payer := benePayerGen().Draw(rt, "payer")
		rec := beneRecordGen().Draw(rt, "record")
		next := beneRecordGen().Draw(rt, "next")
		next.ID = rec.ID
		now := env.clock.Now().Unix()

		want := beneStored(rec, now)
		given := beneCopy(rec)
		beneMustCreate(rt, bs, payer, given)
		vandalize(&given) // the caller reuses the record it passed
		beneWantGet(rt, bs, payer, rec.ID, want)

		got, err := bs.GetBeneficiary(ctx, payer, rec.ID)
		if err != nil {
			rt.Fatalf("GetBeneficiary = %v, want nil", err)
		}
		vandalize(&got)
		beneWantGet(rt, bs, payer, rec.ID, want)

		listed, err := bs.ListBeneficiaries(ctx, payer)
		if err != nil || len(listed) != 1 {
			rt.Fatalf("ListBeneficiaries = %#v, %v, want one record", listed, err)
		}
		vandalize(&listed[0])
		listed[0].ID = "vandal"
		beneWantGet(rt, bs, payer, rec.ID, want)
		beneWantList(rt, bs, payer, []BeneficiaryRecord{want})

		// The same holds for a replace.
		wantNext := beneStored(next, now)
		wantNext.Version = want.Version + 1
		given = beneCopy(next)
		if err := bs.ReplaceBeneficiary(ctx, payer, rec.ID, want.Version, given); err != nil {
			rt.Fatalf("ReplaceBeneficiary = %v, want nil", err)
		}
		vandalize(&given)
		beneWantGet(rt, bs, payer, rec.ID, wantNext)
	})
}

// Feature: risk-intelligence, Property 15: The beneficiary store contract
//
// For any records written for several payers and any page size, one list sends
// one Query on the partition of the payer (one more only for each full page the
// response holds), asks for the base table, ascending and strongly consistent,
// with no index, no Limit, no filter and no projection, sends no other request,
// and returns exactly the payer's unexpired records in ascending id order: no
// record of another payer, no vault item and no history event, even when their
// names look like the payer's partition.
//
// **Validates: Requirements 20.2, 20.4**
func TestDynamoDBBeneficiaryListIsOneQueryPerPageOnThePayersPartition(t *testing.T) {
	contractCheckDynamo(t, func(rt *rapid.T, env *contractEnv) {
		ctx := context.Background()
		bs := beneficiaryOf(rt, env)
		hs := historyOf(rt, env)
		m := newBeneModel(env.clock.Now().Unix())

		// seed writes up to five records under every payer, and a vault and an
		// event named like the payer, so that other families sit beside them.
		seed := func(batch string) {
			for _, p := range benePayers {
				for _, id := range rapid.SliceOfN(beneIDGen(), 0, 5).Draw(rt, batch+"_ids_of_"+p) {
					rec := beneRecordGen().Draw(rt, batch+"_record")
					rec.ID = id
					if err := bs.CreateBeneficiary(ctx, p, rec); err == nil {
						_ = m.create(p, rec, true)
					}
				}
			}
		}
		seed("first")
		for _, p := range benePayers {
			contractMustCreate(rt, env.vs, examplesRecord("v-"+p, StateLocked15M))
			historyMustAppend(rt, hs, p, HistoryEvent{ID: "e", At: 1, AmountINR: 1})
		}
		// Move the clock to the second before, at and after the expiry of the first
		// batch, so that some records are expired and still stored.
		env.clock.Advance(time.Duration(rapid.SampledFrom([]int64{0, beneTTL - 1, beneTTL, beneTTL + 1}).Draw(rt, "advance_s")) * time.Second)
		m.now = env.clock.Now().Unix()
		seed("second")

		payer := benePayerGen().Draw(rt, "list_payer")
		pageSize := rapid.IntRange(0, 4).Draw(rt, "page_size")
		env.fake.pageSize = pageSize

		before := env.fake.queryCalls
		puts, gets, deletes := env.fake.putCalls, env.fake.getCalls, env.fake.deleteCalls
		beneWantList(rt, bs, payer, m.list(payer))

		// Each page costs one Query, and a full page is followed by one more.
		inPartition := len(env.fake.rawItemsUnder("BENE#" + payer))
		wantQueries := 1
		if pageSize > 0 {
			wantQueries = inPartition/pageSize + 1
		}
		if n := env.fake.queryCalls - before; n != wantQueries {
			rt.Fatalf("list sent %d Query requests for %d items with a page of %d, want %d", n, inPartition, pageSize, wantQueries)
		}
		if env.fake.putCalls != puts || env.fake.getCalls != gets || env.fake.deleteCalls != deletes {
			rt.Fatalf("a list sent other requests: puts %d->%d, gets %d->%d, deletes %d->%d",
				puts, env.fake.putCalls, gets, env.fake.getCalls, deletes, env.fake.deleteCalls)
		}

		q := env.fake.lastQuery
		wantValues := map[string]types.AttributeValue{":pk": &types.AttributeValueMemberS{Value: "BENE#" + payer}}
		switch {
		case q.TableName == nil || *q.TableName != contractTable:
			rt.Fatalf("TableName = %v, want %q", q.TableName, contractTable)
		case q.KeyConditionExpression == nil || *q.KeyConditionExpression != "PK = :pk":
			rt.Fatalf("KeyConditionExpression = %v, want %q", q.KeyConditionExpression, "PK = :pk")
		case !reflect.DeepEqual(q.ExpressionAttributeValues, wantValues):
			rt.Fatalf("ExpressionAttributeValues = %v, want %v", q.ExpressionAttributeValues, wantValues)
		case q.ScanIndexForward == nil || !*q.ScanIndexForward:
			rt.Fatalf("ScanIndexForward = %v, want true (ascending id order)", q.ScanIndexForward)
		case q.ConsistentRead == nil || !*q.ConsistentRead:
			rt.Fatalf("ConsistentRead = %v, want true", q.ConsistentRead)
		case q.Limit != nil:
			rt.Fatalf("Limit = %v, want none: the list reads every page", *q.Limit)
		case q.IndexName != nil || q.FilterExpression != nil || q.ProjectionExpression != nil ||
			q.Select != "" || len(q.ExpressionAttributeNames) != 0:
			rt.Fatalf("Query names an index, a filter, a projection or names: %+v", q)
		}
	})
}

// Feature: risk-intelligence, Property 15: The beneficiary store contract
//
// DynamoDBStore only. For any record, create writes one item with PK
// BENE#<payer> and SK B#<id>, with the attributes in their documented types
// (expires_at and version as Numbers, the optional ones absent when absent), and
// no item in the vault or history families. Create sends one PutItem guarded by
// createCondition. Replace sends one PutItem guarded by replaceCondition with the
// expected version and the clock, and writes version expected + 1 and a fresh
// expires_at. Delete sends one DeleteItem with the same guard. Get sends one
// consistent GetItem.
//
// **Validates: Requirements 20.2, 20.3, 20.4, 20.12**
func TestDynamoDBBeneficiaryItemLayoutAndRequests(t *testing.T) {
	num := func(item map[string]types.AttributeValue, name string) string {
		n, ok := item[name].(*types.AttributeValueMemberN)
		if !ok {
			return fmt.Sprintf("<%T>", item[name])
		}
		return n.Value
	}

	// checkItem stops the test unless the stored item is want in the layout above.
	checkItem := func(rt *rapid.T, env *contractEnv, payer string, want BeneficiaryRecord, id string) {
		rt.Helper()
		item := env.fake.rawItemAt("BENE#"+payer, "B#"+id)
		if item == nil {
			rt.Fatalf("no item under BENE#%s / B#%s", payer, id)
		}
		if num(item, "expires_at") != strconv.FormatInt(want.ExpiresAt, 10) {
			rt.Fatalf("expires_at = %s, want N %d", num(item, "expires_at"), want.ExpiresAt)
		}
		if num(item, "version") != strconv.Itoa(want.Version) {
			rt.Fatalf("version = %s, want N %d", num(item, "version"), want.Version)
		}
		for _, name := range []string{"added_at", "payment_count"} {
			if _, ok := item[name].(*types.AttributeValueMemberN); !ok {
				rt.Fatalf("%s = %T, want a Number", name, item[name])
			}
		}
		if _, ok := item["pressure_flag"].(*types.AttributeValueMemberBOOL); !ok {
			rt.Fatalf("pressure_flag = %T, want a Bool", item["pressure_flag"])
		}
		for _, name := range []string{"beneficiary_id", "label", "identifier_masked", "trust_level", "protection_level"} {
			if _, ok := item[name].(*types.AttributeValueMemberS); !ok {
				rt.Fatalf("%s = %T, want a String", name, item[name])
			}
		}
		if _, present := item["verified_at"]; present != (want.VerifiedAt != nil) {
			rt.Fatalf("verified_at present = %v, want %v", present, want.VerifiedAt != nil)
		}
		if _, present := item["first_payment_amount"]; present != (want.FirstPaymentINR != nil) {
			rt.Fatalf("first_payment_amount present = %v, want %v", present, want.FirstPaymentINR != nil)
		}
		history, present := item["history"]
		if present != (want.History != nil) {
			rt.Fatalf("history present = %v, want %v", present, want.History != nil)
		}
		if present {
			if list, ok := history.(*types.AttributeValueMemberL); !ok || len(list.Value) != len(want.History) {
				rt.Fatalf("history = %#v, want a list of %d entries", history, len(want.History))
			}
		}
		known := map[string]bool{
			"PK": true, "SK": true, "beneficiary_id": true, "label": true, "identifier_masked": true, "added_at": true,
			"verified_at": true, "trust_level": true, "protection_level": true, "pressure_flag": true, "version": true,
			"payment_count": true, "first_payment_amount": true, "history": true, "expires_at": true,
		}
		for name := range item {
			if !known[name] {
				rt.Fatalf("the item holds an attribute %q that the layout does not name", name)
			}
		}
	}

	contractCheckDynamo(t, func(rt *rapid.T, env *contractEnv) {
		ctx := context.Background()
		bs := beneficiaryOf(rt, env)
		payer := benePayerGen().Draw(rt, "payer")
		rec := beneRecordGen().Draw(rt, "record")
		next := beneRecordGen().Draw(rt, "next")
		now := env.clock.Now().Unix()

		// Create.
		beneMustCreate(rt, bs, payer, rec)
		if got := env.fake.itemCount(); got != 1 {
			rt.Fatalf("itemCount = %d after a create, want 1 item", got)
		}
		put := env.fake.lastPut
		if env.fake.putCalls != 1 || put.ConditionExpression == nil || *put.ConditionExpression != "attribute_not_exists(PK)" ||
			len(put.ExpressionAttributeNames) != 0 || len(put.ExpressionAttributeValues) != 0 {
			rt.Fatalf("create sent %d PutItem requests, the last with %+v, want one guarded by createCondition", env.fake.putCalls, put)
		}
		checkItem(rt, env, payer, beneStored(rec, now), rec.ID)
		if env.fake.rawItem(payer) != nil || len(env.fake.rawItemsUnder("HIST#"+payer)) != 0 {
			rt.Fatalf("a create left an item in the vault or history family")
		}

		// Replace, a moment later.
		env.clock.Advance(7 * time.Second)
		later := env.clock.Now().Unix()
		puts := env.fake.putCalls
		if err := bs.ReplaceBeneficiary(ctx, payer, rec.ID, rec.Version, next); err != nil {
			rt.Fatalf("ReplaceBeneficiary = %v, want nil", err)
		}
		put = env.fake.lastPut
		wantNames := map[string]string{"#v": "version", "#e": "expires_at"}
		wantValues := map[string]types.AttributeValue{
			":expected": &types.AttributeValueMemberN{Value: strconv.Itoa(rec.Version)},
			":now":      &types.AttributeValueMemberN{Value: strconv.FormatInt(later, 10)},
		}
		if env.fake.putCalls != puts+1 || put.ConditionExpression == nil ||
			*put.ConditionExpression != "attribute_exists(PK) AND #v = :expected AND #e >= :now" ||
			!reflect.DeepEqual(put.ExpressionAttributeNames, wantNames) || !reflect.DeepEqual(put.ExpressionAttributeValues, wantValues) {
			rt.Fatalf("replace sent %d PutItem requests, the last with %+v, want one guarded by replaceCondition for version %d at %d",
				env.fake.putCalls-puts, put, rec.Version, later)
		}
		wantNext := beneStored(next, later)
		wantNext.Version = rec.Version + 1
		checkItem(rt, env, payer, wantNext, rec.ID)

		// Get: one consistent GetItem, and nothing else.
		puts, gets := env.fake.putCalls, env.fake.getCalls
		// A replace keys the item by the id, so the record keeps next's own ID.
		beneWantGet(rt, bs, payer, rec.ID, wantNext)
		get := env.fake.lastGet
		if env.fake.getCalls != gets+1 || env.fake.putCalls != puts || get.ConsistentRead == nil || !*get.ConsistentRead {
			rt.Fatalf("get sent %d GetItem requests (consistent %v), want one consistent read", env.fake.getCalls-gets, get.ConsistentRead)
		}

		// Delete: one DeleteItem with the guard, and the item is gone.
		if err := bs.DeleteBeneficiary(ctx, payer, rec.ID, wantNext.Version); err != nil {
			rt.Fatalf("DeleteBeneficiary = %v, want nil", err)
		}
		del := env.fake.lastDelete
		wantValues[":expected"] = &types.AttributeValueMemberN{Value: strconv.Itoa(wantNext.Version)}
		if env.fake.deleteCalls != 1 || del.ConditionExpression == nil || *del.ConditionExpression != "attribute_exists(PK) AND #v = :expected AND #e >= :now" ||
			!reflect.DeepEqual(del.ExpressionAttributeNames, wantNames) || !reflect.DeepEqual(del.ExpressionAttributeValues, wantValues) {
			rt.Fatalf("delete sent %d DeleteItem requests, the last with %+v, want one guarded by replaceCondition for version %d",
				env.fake.deleteCalls, del, wantNext.Version)
		}
		if got := env.fake.itemCount(); got != 0 {
			rt.Fatalf("itemCount = %d after a delete, want 0: the record and its history are one item", got)
		}
	})
}

// beneEchoing returns an error whose text repeats every value of rec, the way an
// SDK error can echo the request that failed. If a store put this text in its own
// message, a leak check would find the values.
func beneEchoing(payer string, rec BeneficiaryRecord) error {
	var amounts []string
	for _, e := range rec.History {
		amounts = append(amounts, strconv.FormatInt(e.AmountINR, 10))
	}
	return fmt.Errorf("request failed for payer %s id %s label %s identifier %s amounts %s",
		payer, rec.ID, rec.Label, rec.IdentifierMasked, strings.Join(amounts, ","))
}

// Feature: risk-intelligence, Property 15: The beneficiary store contract
//
// DynamoDBStore only. When the client fails with an error whose own text repeats
// the record's label, identifier and amounts, every store method returns an error
// that wraps it, whose whole text is the operation, the id and a fixed
// description, and that holds none of those values: for a failed create, get,
// list, replace and delete, a failed second page of a list, a failed follow-up
// read after a refused condition, and an item that cannot be decoded. A failed
// call changes nothing.
//
// **Validates: Requirements 20.7, 24.5**
func TestDynamoDBBeneficiaryErrorsHoldOnlyTheOperationAndTheID(t *testing.T) {
	ctx := context.Background()

	contractCheckDynamo(t, func(rt *rapid.T, env *contractEnv) {
		bs := beneficiaryOf(rt, env)
		payer := benePayerGen().Draw(rt, "payer")
		rec := beneRecordGen().Draw(rt, "record")
		next := beneRecordGen().Draw(rt, "next")
		next.ID = rec.ID
		// A label no fixed text can contain, and amounts of 7 or more digits.
		rec.Label = rapid.StringMatching(`^LBL[A-Z]{9}$`).Draw(rt, "canary_label")
		rec.History = append(rec.History, HistoryEntry{At: 1, Kind: "PAYMENT", AmountINR: 8_765_432})
		injected := beneEchoing(payer, rec)

		// leakCheck stops the test unless err is the fixed text for op and id, wraps the
		// injected error and holds no value of rec.
		leakCheck := func(what string, err error, op, id, fixed string) {
			rt.Helper()
			if err == nil || !errors.Is(err, injected) {
				rt.Fatalf("%s: error = %v, want one wrapping the injected error", what, err)
			}
			if got, want := err.Error(), op+" "+id+": "+fixed; got != want {
				rt.Fatalf("%s: error text = %q, want %q", what, got, want)
			}
			secrets := []string{rec.Label, rec.IdentifierMasked, "8765432"}
			if len(payer) >= 3 {
				secrets = append(secrets, payer) // shorter keys can occur in the fixed text
			}
			for _, secret := range secrets {
				if strings.Contains(err.Error(), secret) {
					rt.Fatalf("%s: error text %q holds %q", what, err.Error(), secret)
				}
			}
		}
		const request, decode = "dynamodb request failed", "dynamodb item decode failed"

		// Create fails at the put, and stores nothing.
		env.fake.failNext(fakeDynamoDBOpPutItem, injected)
		leakCheck("create", bs.CreateBeneficiary(ctx, payer, rec), opCreateBeneficiary, rec.ID, request)
		if env.fake.itemCount() != 0 {
			rt.Fatalf("a failed create left %d items", env.fake.itemCount())
		}
		beneMustCreate(rt, bs, payer, rec)
		want := beneStored(rec, env.clock.Now().Unix())

		// Get fails at the read.
		env.fake.failNext(fakeDynamoDBOpGetItem, injected)
		_, err := bs.GetBeneficiary(ctx, payer, rec.ID)
		leakCheck("get", err, opGetBeneficiary, rec.ID, request)

		// List fails at the first Query, and at the second page.
		env.fake.failNext(fakeDynamoDBOpQuery, injected)
		listed, err := bs.ListBeneficiaries(ctx, payer)
		leakCheck("list", err, opListBeneficiaries, "-", request)
		if listed != nil {
			rt.Fatalf("a failed list returned %#v, want no records", listed)
		}
		env.fake.pageSize = 1
		env.fake.failOn(fakeDynamoDBOpQuery, 2, injected)
		listed, err = bs.ListBeneficiaries(ctx, payer)
		leakCheck("list, second page", err, opListBeneficiaries, "-", request)
		if listed != nil {
			rt.Fatalf("a list that failed on its second page returned %#v, want no records", listed)
		}
		env.fake.pageSize = 0

		// Replace fails at the put, and after a refused condition at the read.
		env.fake.failNext(fakeDynamoDBOpPutItem, injected)
		leakCheck("replace", bs.ReplaceBeneficiary(ctx, payer, rec.ID, rec.Version, next), opReplaceBeneficiary, rec.ID, request)
		env.fake.failNextConditional(fakeDynamoDBOpPutItem)
		env.fake.failNext(fakeDynamoDBOpGetItem, injected)
		leakCheck("replace, follow-up read", bs.ReplaceBeneficiary(ctx, payer, rec.ID, rec.Version, next), opReplaceBeneficiary, rec.ID, request)

		// Delete fails at the client (the wrapper), and after a refused condition at the read.
		faulty := &deleteFaultClient{DynamoDBClient: env.fake, err: injected}
		ds := NewDynamoDBStore(faulty, contractTable)
		ds.now = env.clock.Now
		leakCheck("delete", ds.DeleteBeneficiary(ctx, payer, rec.ID, rec.Version), opDeleteBeneficiary, rec.ID, request)
		faulty.err = &types.ConditionalCheckFailedException{}
		env.fake.failNext(fakeDynamoDBOpGetItem, injected)
		leakCheck("delete, follow-up read", ds.DeleteBeneficiary(ctx, payer, rec.ID, rec.Version), opDeleteBeneficiary, rec.ID, request)

		// None of the failed calls changed the stored record.
		beneWantGet(rt, bs, payer, rec.ID, want)

		// An item that cannot be decoded gives the fixed decode text and no record.
		corrupt := env.fake.rawItemAt("BENE#"+payer, "B#"+rec.ID)
		corrupt["version"] = &types.AttributeValueMemberS{Value: rec.Label}
		if _, err := env.fake.PutItem(ctx, fakeDynamoDBPutInput(corrupt)); err != nil {
			rt.Fatalf("seed a corrupt item: %v", err)
		}
		_, err = bs.GetBeneficiary(ctx, payer, rec.ID)
		if err == nil || err.Error() != opGetBeneficiary+" "+rec.ID+": "+decode || strings.Contains(err.Error(), rec.Label) {
			rt.Fatalf("get of a corrupt item = %v, want the fixed decode text", err)
		}
		listed, err = bs.ListBeneficiaries(ctx, payer)
		if err == nil || err.Error() != opListBeneficiaries+" -: "+decode || listed != nil {
			rt.Fatalf("list with a corrupt item = %#v, %v, want no records and the fixed decode text", listed, err)
		}
	})
}
