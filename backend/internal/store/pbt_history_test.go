package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"pgregory.net/rapid"
)

// Property 14 is checked by the tests of this file. Each runs against
// MemoryStore and against DynamoDBStore over the fake through contractCheck
// (or contractCheckDynamo for the item layout), so both adapters face the same
// generated inputs. The model below is written separately from the stores: it
// sorts and filters with plain comparisons and shares no code with them.

// historyOf returns the HistoryStore of env, or stops the test.
func historyOf(t rapid.TB, env *contractEnv) HistoryStore {
	t.Helper()
	hs, ok := env.vs.(HistoryStore)
	if !ok {
		t.Fatalf("%T does not implement HistoryStore", env.vs)
	}
	return hs
}

// historyNow is the time of the contract clock before any test moves it, in
// epoch seconds.
func historyNow() int64 { return contractEpoch().Unix() }

// historyPayers are the payer keys the generators use. They include keys that
// look like the other key family and keys with a separator, so a collision
// between partitions would show.
var historyPayers = []string{PayerKey, "other-payer", "", "VAULT#x", "a#b"}

func historyPayerGen() *rapid.Generator[string] {
	return rapid.SampledFrom(historyPayers)
}

// historyIDGen draws ids from a small pool, so events share ids and times.
func historyIDGen() *rapid.Generator[string] {
	return rapid.StringMatching(`^[a-c]{1,3}$`)
}

// historyAtGen draws event times: the extremes of int64, a narrow window around
// the clock that makes equal times common, a wider window, and any int64.
func historyAtGen() *rapid.Generator[int64] {
	now := historyNow()
	return rapid.OneOf(
		rapid.SampledFrom([]int64{math.MinInt64, math.MinInt64 + 1, -1, 0, 1, math.MaxInt64 - 1, math.MaxInt64}),
		rapid.Int64Range(now-3, now+3),
		rapid.Int64Range(now-1000, now+1000),
		rapid.Int64(),
	)
}

// historyExpiryGen draws an ExpiresAt: 0 (never), values at, before and after
// the clock, and any int64.
func historyExpiryGen() *rapid.Generator[int64] {
	now := historyNow()
	return rapid.OneOf(
		rapid.Just(int64(0)),
		rapid.Int64Range(now-3, now+3),
		rapid.SampledFrom([]int64{math.MinInt64, -1, 1, math.MaxInt64}),
		rapid.Int64(),
	)
}

func historyAmountGen() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{0, 1, 450, 85000, math.MaxInt64, math.MinInt64, -1}),
		rapid.Int64(),
	)
}

// historyRefGen draws an empty reference or a masked one of any format.
func historyRefGen() *rapid.Generator[string] {
	return contractOptional(rapid.OneOf(contractAccountGen(), contractPhoneGen(), contractUPIGen()))
}

// historyEventGen draws a valid event with any int64 time, amount and expiry.
func historyEventGen() *rapid.Generator[HistoryEvent] {
	return rapid.Custom(func(t *rapid.T) HistoryEvent {
		return HistoryEvent{
			ID:             historyIDGen().Draw(t, "id"),
			At:             historyAtGen().Draw(t, "at"),
			AmountINR:      historyAmountGen().Draw(t, "amount_inr"),
			BeneficiaryRef: historyRefGen().Draw(t, "beneficiary_ref"),
			ExpiresAt:      historyExpiryGen().Draw(t, "expires_at"),
		}
	})
}

// historyPlaced is one event with the payer it is written under.
type historyPlaced struct {
	payer string
	ev    HistoryEvent
}

func historyPlacedGen() *rapid.Generator[historyPlaced] {
	return rapid.Custom(func(t *rapid.T) historyPlaced {
		return historyPlaced{
			payer: historyPayerGen().Draw(t, "payer"),
			ev:    historyEventGen().Draw(t, "event"),
		}
	})
}

// historyLimitGen draws a read limit: below 1, the usual values, and the
// largest int.
func historyLimitGen() *rapid.Generator[int] {
	return rapid.OneOf(
		rapid.SampledFrom([]int{math.MinInt32, -1, 0, 1, 2, 3, 100, math.MaxInt32, math.MaxInt}),
		rapid.IntRange(1, 50),
	)
}

// historyModel is the reference: the events accepted so far, per payer.
type historyModel struct {
	byPayer map[string][]HistoryEvent
}

func newHistoryModel() *historyModel {
	return &historyModel{byPayer: make(map[string][]HistoryEvent)}
}

// add records ev under payer and reports false, recording nothing, when an event
// with the same time and id is already there.
func (m *historyModel) add(payer string, ev HistoryEvent) bool {
	for _, old := range m.byPayer[payer] {
		if old.At == ev.At && old.ID == ev.ID {
			return false
		}
	}
	m.byPayer[payer] = append(m.byPayer[payer], ev)
	return true
}

// ascending returns the events of payer ordered by time and then by id.
func (m *historyModel) ascending(payer string) []HistoryEvent {
	out := append([]HistoryEvent{}, m.byPayer[payer]...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].At != out[j].At {
			return out[i].At < out[j].At
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func historyUnexpired(ev HistoryEvent, now int64) bool {
	return ev.ExpiresAt == 0 || ev.ExpiresAt >= now
}

// recent is what RecentHistory must return: the limit newest events, with the
// expired ones dropped afterwards, oldest first.
func (m *historyModel) recent(payer string, limit int, now int64) []HistoryEvent {
	out := []HistoryEvent{}
	all := m.ascending(payer)
	if limit < 1 {
		return out
	}
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	for _, ev := range all {
		if historyUnexpired(ev, now) {
			out = append(out, ev)
		}
	}
	return out
}

// recentUnexpired is the limit newest events among the unexpired ones. It equals
// recent when expiry never reorders the events, as for events that the service
// writes with HistoryExpiry.
func (m *historyModel) recentUnexpired(payer string, limit int, now int64) []HistoryEvent {
	out := []HistoryEvent{}
	if limit < 1 {
		return out
	}
	for _, ev := range m.ascending(payer) {
		if historyUnexpired(ev, now) {
			out = append(out, ev)
		}
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// historyCalls are the client calls the fake has seen. They stay zero for
// MemoryStore.
type historyCalls struct{ puts, gets, queries int }

func historyCallsOf(env *contractEnv) historyCalls {
	if env.fake == nil {
		return historyCalls{}
	}
	return historyCalls{puts: env.fake.putCalls, gets: env.fake.getCalls, queries: env.fake.queryCalls}
}

// historyDuplicateText is the exact text of the error for a repeated event.
func historyDuplicateText(id string) string { return "append_history " + id + ": vault already exists" }

// historyMustAppend writes ev and stops the test on an error.
func historyMustAppend(t rapid.TB, hs HistoryStore, payer string, ev HistoryEvent) {
	t.Helper()
	if err := hs.AppendHistory(context.Background(), payer, ev); err != nil {
		t.Fatalf("AppendHistory(payer %q, id %q, at %d) = %v, want nil", payer, ev.ID, ev.At, err)
	}
}

// historyWantRead stops the test unless RecentHistory returns want.
func historyWantRead(t rapid.TB, hs HistoryStore, payer string, limit int, want []HistoryEvent) {
	t.Helper()
	got, err := hs.RecentHistory(context.Background(), payer, limit)
	if err != nil {
		t.Fatalf("RecentHistory(payer %q, limit %d) = %v, want nil", payer, limit, err)
	}
	if got == nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("RecentHistory(payer %q, limit %d) =\n  %#v\nwant\n  %#v", payer, limit, got, want)
	}
}

// Feature: risk-intelligence, Property 14: The history store contract
//
// For any events written under any payers, including repeats of a time and id:
// a write succeeds the first time and returns ErrAlreadyExists the second, the
// stored event staying as first written; RecentHistory returns, for each
// payer and any limit, the unexpired events among the limit newest, in
// ascending time and id order, equal in id, time, amount, reference and expiry
// to what was written (an empty reference, the extreme int64 values and the
// events of other payers included); an event is hidden when its ExpiresAt is
// not 0 and earlier than the clock, kept when equal, and never hidden when 0; a
// limit below 1 gives an empty list and DynamoDBStore sends no request, and any
// other limit costs exactly one Query and nothing else.
//
// **Validates: Requirements 12.10, 13.3, 13.7, 13.11, 13.12, 13.13**
func TestHistoryContractWritesReadsAndExpiry(t *testing.T) {
	contractCheck(t, func(rt *rapid.T, env *contractEnv) {
		ctx := context.Background()
		hs := historyOf(rt, env)
		model := newHistoryModel()

		placed := rapid.SliceOfN(historyPlacedGen(), 0, 40).Draw(rt, "events")
		for _, p := range placed {
			err := hs.AppendHistory(ctx, p.payer, p.ev)
			if model.add(p.payer, p.ev) {
				if err != nil {
					rt.Fatalf("AppendHistory(payer %q, id %q, at %d) = %v, want nil", p.payer, p.ev.ID, p.ev.At, err)
				}
				continue
			}
			if !errors.Is(err, ErrAlreadyExists) || err.Error() != historyDuplicateText(p.ev.ID) {
				rt.Fatalf("a repeated (payer, at, id) returned %v, want ErrAlreadyExists with the fixed text", err)
			}
		}

		// Move the clock so that ExpiresAt values around it fall on both sides.
		env.clock.Advance(time.Duration(rapid.Int64Range(0, 3).Draw(rt, "advance_s")) * time.Second)
		now := env.clock.Now().Unix()

		readers := append(append([]string{}, historyPayers...), "nobody")
		limits := rapid.SliceOfN(historyLimitGen(), 1, 4).Draw(rt, "limits")
		for _, payer := range readers {
			for _, limit := range limits {
				before := historyCallsOf(env)
				historyWantRead(rt, hs, payer, limit, model.recent(payer, limit, now))

				want := before
				if env.fake != nil && limit >= 1 {
					want.queries++
				}
				if got := historyCallsOf(env); got != want {
					rt.Fatalf("client calls after RecentHistory(limit %d) = %+v, want %+v", limit, got, want)
				}
			}
		}
	})
}

// Feature: risk-intelligence, Property 14: The history store contract
//
// For events written the way the service writes them, with ExpiresAt equal to
// HistoryExpiry(At), the unexpired events among the limit newest are also the
// limit newest unexpired events, because the events that expire are always the
// oldest. This is the form Requirement 13.11 states.
//
// **Validates: Requirements 12.10, 13.7, 13.11**
func TestHistoryServiceShapedEventsReturnTheNewestUnexpired(t *testing.T) {
	contractCheck(t, func(rt *rapid.T, env *contractEnv) {
		hs := historyOf(rt, env)
		model := newHistoryModel()
		now := historyNow()

		atGen := rapid.OneOf(
			rapid.Int64Range(now-HistoryRetentionSeconds-5, now+5),
			rapid.SampledFrom([]int64{math.MinInt64, math.MaxInt64 - HistoryRetentionSeconds - 1, math.MaxInt64}),
		)
		events := rapid.SliceOfN(rapid.Custom(func(t *rapid.T) HistoryEvent {
			ev := historyEventGen().Draw(t, "event")
			ev.At = atGen.Draw(t, "at")
			ev.ExpiresAt = HistoryExpiry(ev.At)
			return ev
		}), 0, 40).Draw(rt, "events")
		for _, ev := range events {
			if model.add(PayerKey, ev) {
				historyMustAppend(rt, hs, PayerKey, ev)
			}
		}

		env.clock.Advance(time.Duration(rapid.Int64Range(0, 10).Draw(rt, "advance_s")) * time.Second)
		clockNow := env.clock.Now().Unix()
		for _, limit := range rapid.SliceOfN(rapid.IntRange(1, 50), 1, 4).Draw(rt, "limits") {
			want := model.recentUnexpired(PayerKey, limit, clockNow)
			if viaModel := model.recent(PayerKey, limit, clockNow); !reflect.DeepEqual(want, viaModel) {
				rt.Fatalf("the two readings of the limit differ for service-shaped events:\n  %#v\n  %#v", want, viaModel)
			}
			historyWantRead(rt, hs, PayerKey, limit, want)
		}
	})
}

// Feature: risk-intelligence, Property 14: The history store contract
//
// A write whose (payer, time, id) is already stored returns ErrAlreadyExists
// and leaves the stored event as it was, whatever its other fields. The same
// time and id under another payer, the same id at another time and another id
// at the same time are different events and all succeed.
//
// **Validates: Requirements 13.4, 13.12**
func TestHistoryDuplicateKeepsTheStoredEvent(t *testing.T) {
	contractCheck(t, func(rt *rapid.T, env *contractEnv) {
		ctx := context.Background()
		hs := historyOf(rt, env)

		first := historyEventGen().Draw(rt, "first")
		first.ExpiresAt = 0 // never hidden, so every read shows it
		second := historyEventGen().Draw(rt, "second")
		second.At, second.ID, second.ExpiresAt = first.At, first.ID, 0
		if second == first {
			second.AmountINR = first.AmountINR ^ 1
		}

		historyMustAppend(rt, hs, PayerKey, first)
		err := hs.AppendHistory(ctx, PayerKey, second)
		if !errors.Is(err, ErrAlreadyExists) || err.Error() != historyDuplicateText(first.ID) {
			rt.Fatalf("a repeated event returned %v, want ErrAlreadyExists with the fixed text", err)
		}
		historyWantRead(rt, hs, PayerKey, 10, []HistoryEvent{first})

		// Different keys do not clash with it.
		otherPayer := second
		historyMustAppend(rt, hs, "someone-else", otherPayer)
		otherTime, otherID := first, first
		otherTime.At = first.At ^ 1
		otherID.ID = first.ID + "z"
		historyMustAppend(rt, hs, PayerKey, otherTime)
		historyMustAppend(rt, hs, PayerKey, otherID)

		model := newHistoryModel()
		for _, ev := range []HistoryEvent{first, otherTime, otherID} {
			model.add(PayerKey, ev)
		}
		historyWantRead(rt, hs, PayerKey, 10, model.recent(PayerKey, 10, env.clock.Now().Unix()))
		historyWantRead(rt, hs, "someone-else", 10, []HistoryEvent{otherPayer})
	})
}

// historyUnmaskedRefGen draws a reference that is not empty and not masked.
func historyUnmaskedRefGen() *rapid.Generator[string] {
	return rapid.OneOf(
		rapid.StringMatching(`^[0-9]{9,18}$`),
		rapid.StringMatching(`^\+91[0-9]{10}$`),
		rapid.StringMatching(`^[a-z]{3,8}@[a-z]{2,6}$`),
		rapid.SampledFrom([]string{
			" ", "XXXX-XXXX-482", "XXXX-XXXX-48211", "xxxx-xxxx-4821", " XXXX-XXXX-4821",
			"XXXX-XXXX-4821\n", "+91-XXXXX3210", "r**@okaxis", "ra***@okaxis", "r***@",
			"[REDACTED]", "9876543210",
		}),
	)
}

// Feature: risk-intelligence, Property 14: The history store contract
//
// A write whose reference is not empty and not a masked identifier returns
// ErrUnmaskedIdentifier with a text that holds only the operation, the event id
// and the sentinel, and stores nothing. DynamoDBStore sends no request. The check
// comes before the duplicate check, so even a repeated key reports the
// unmasked reference.
//
// **Validates: Requirements 13.9, 24.5**
func TestHistoryUnmaskedReferenceWritesNothing(t *testing.T) {
	contractCheck(t, func(rt *rapid.T, env *contractEnv) {
		ctx := context.Background()
		hs := historyOf(rt, env)

		stored := historyEventGen().Draw(rt, "stored")
		stored.ExpiresAt = 0
		historyMustAppend(rt, hs, PayerKey, stored)
		before := historyCallsOf(env)
		itemsBefore := 0
		if env.fake != nil {
			itemsBefore = env.fake.itemCount()
		}

		bad := historyEventGen().Draw(rt, "bad")
		bad.ID = contractIDGen().Draw(rt, "bad_id")
		bad.BeneficiaryRef = historyUnmaskedRefGen().Draw(rt, "unmasked_ref")
		if rapid.Bool().Draw(rt, "repeat_key") {
			bad.At, bad.ID = stored.At, stored.ID
		}

		err := hs.AppendHistory(ctx, PayerKey, bad)
		if !errors.Is(err, ErrUnmaskedIdentifier) {
			rt.Fatalf("AppendHistory with reference %q = %v, want ErrUnmaskedIdentifier", bad.BeneficiaryRef, err)
		}
		if want := "append_history " + bad.ID + ": unmasked identifier"; err.Error() != want {
			rt.Fatalf("error text = %q, want %q", err.Error(), want)
		}
		if got := historyCallsOf(env); got != before {
			rt.Fatalf("client calls after a refused write = %+v, want %+v (no request)", got, before)
		}
		if env.fake != nil && env.fake.itemCount() != itemsBefore {
			rt.Fatalf("itemCount = %d after a refused write, want %d", env.fake.itemCount(), itemsBefore)
		}
		historyWantRead(rt, hs, PayerKey, 10, []HistoryEvent{stored})
	})
}

// Feature: risk-intelligence, Property 14: The history store contract
//
// Writes made at the same moment by many goroutines, with different ids and
// often equal times, all survive: after they return, a read with a limit above
// their number returns every one of them, and reads made during the writes
// never fail.
//
// **Validates: Requirements 12.9, 13.4, 13.8**
func TestHistoryConcurrentWritesAllSurvive(t *testing.T) {
	contractCheck(t, func(rt *rapid.T, env *contractEnv) {
		ctx := context.Background()
		hs := historyOf(rt, env)

		n := rapid.IntRange(2, 60).Draw(rt, "writers")
		model := newHistoryModel()
		events := make([]HistoryEvent, n)
		for i := range events {
			ev := historyEventGen().Draw(rt, "event")
			ev.ID = fmt.Sprintf("w%02d", i) // distinct ids, so no write is a repeat
			ev.At = rapid.Int64Range(historyNow()-2, historyNow()+2).Draw(rt, "at")
			ev.ExpiresAt = rapid.SampledFrom([]int64{0, historyNow() + 1000}).Draw(rt, "expires_at")
			events[i] = ev
			model.add(PayerKey, ev)
		}

		var (
			ready, done sync.WaitGroup
			start       = make(chan struct{})
			errs        = make([]error, n+2)
		)
		ready.Add(n + 2)
		done.Add(n + 2)
		for i := range events {
			i := i
			go func() {
				defer done.Done()
				ready.Done()
				<-start
				errs[i] = hs.AppendHistory(ctx, PayerKey, events[i])
			}()
		}
		for r := 0; r < 2; r++ { // two readers overlap the writers
			r := r
			go func() {
				defer done.Done()
				ready.Done()
				<-start
				_, errs[n+r] = hs.RecentHistory(ctx, PayerKey, SnapshotLimit)
			}()
		}
		ready.Wait()
		close(start)
		done.Wait()

		for i, err := range errs {
			if err != nil {
				rt.Fatalf("goroutine %d returned %v, want nil", i, err)
			}
		}
		historyWantRead(rt, hs, PayerKey, n+10, model.recent(PayerKey, n+10, env.clock.Now().Unix()))
	})
}

// Feature: risk-intelligence, Property 14: The history store contract
//
// The sort key orders events by time and then by id for every int64 time, the
// negative and the largest included, and holds each id byte for byte after the
// time, so ids that contain the separator or are prefixes of one another keep
// their order.
//
// **Validates: Requirements 13.3, 13.4**
func TestHistorySortKeyOrdersByTimeThenID(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		idGen := rapid.StringMatching(`^[a-c#]{0,4}$`)
		atGen := rapid.OneOf(historyAtGen(), rapid.SampledFrom([]int64{math.MinInt64, math.MaxInt64, -1, 0, 1}))
		atA, idA := atGen.Draw(rt, "at_a"), idGen.Draw(rt, "id_a")
		atB, idB := atGen.Draw(rt, "at_b"), idGen.Draw(rt, "id_b")

		var want int
		switch {
		case atA < atB:
			want = -1
		case atA > atB:
			want = 1
		default:
			want = strings.Compare(idA, idB)
		}
		keyA, keyB := historySortKey(atA, idA), historySortKey(atB, idB)
		if got := strings.Compare(keyA, keyB); got != want {
			rt.Fatalf("compare(%q, %q) = %d, want %d for (%d, %q) against (%d, %q)", keyA, keyB, got, want, atA, idA, atB, idB)
		}
		if wantLen := len("E#") + 20 + len("#") + len(idA); len(keyA) != wantLen || !strings.HasPrefix(keyA, "E#") || !strings.HasSuffix(keyA, "#"+idA) {
			rt.Fatalf("sort key %q has the wrong shape for (%d, %q)", keyA, atA, idA)
		}
	})
}

// historyBiasedDigits writes at + 2^63 as 20 decimal digits, computed with
// arbitrary-precision integers and no bit tricks.
func historyBiasedDigits(at int64) string {
	biased := new(big.Int).Add(big.NewInt(at), new(big.Int).Lsh(big.NewInt(1), 63))
	return fmt.Sprintf("%020s", biased.String())
}

// Feature: risk-intelligence, Property 14: The history store contract
//
// DynamoDBStore only: each event is its own item. Its PK is "HIST#" and the
// payer, which differs from the PK of every vault, and its SK is "E#", the time
// as 20 digits, "#" and the id. The item holds the five event attributes and
// nothing else, with a reference attribute only when the reference is not empty.
// It is written with the create condition, to the configured table. A vault
// whose id equals the payer lives in another partition: a vault read never
// returns an event, and a history read never returns a vault.
//
// **Validates: Requirements 12.6, 13.3, 13.4, 13.5**
func TestHistoryDynamoDBItemLayout(t *testing.T) {
	contractCheckDynamo(t, func(rt *rapid.T, env *contractEnv) {
		ctx := context.Background()
		hs := historyOf(rt, env)

		payer := contractIDGen().Draw(rt, "payer")
		ev := historyEventGen().Draw(rt, "event")
		ev.ExpiresAt = 0
		vault := contractRecordGen().Draw(rt, "vault")
		vault.ID = payer // the same text as the payer key

		contractMustCreate(rt, env.vs, vault)
		historyMustAppend(rt, hs, payer, ev)

		pk, sk := "HIST#"+payer, "E#"+historyBiasedDigits(ev.At)+"#"+ev.ID
		item := env.fake.rawItemAt(pk, sk)
		if item == nil {
			rt.Fatalf("no item stored under PK %q and SK %q", pk, sk)
		}

		wantAttrs := map[string]types.AttributeValue{
			"PK":         &types.AttributeValueMemberS{Value: pk},
			"SK":         &types.AttributeValueMemberS{Value: sk},
			"event_id":   &types.AttributeValueMemberS{Value: ev.ID},
			"at":         &types.AttributeValueMemberN{Value: strconv.FormatInt(ev.At, 10)},
			"amount_inr": &types.AttributeValueMemberN{Value: strconv.FormatInt(ev.AmountINR, 10)},
			"expires_at": &types.AttributeValueMemberN{Value: "0"},
		}
		if ev.BeneficiaryRef != "" {
			wantAttrs["beneficiary_ref"] = &types.AttributeValueMemberS{Value: ev.BeneficiaryRef}
		}
		if !reflect.DeepEqual(item, wantAttrs) {
			rt.Fatalf("stored item =\n  %v\nwant\n  %v", item, wantAttrs)
		}

		put := env.fake.lastPut
		if put.TableName == nil || *put.TableName != contractTable {
			rt.Fatalf("PutItem table = %v, want %q", put.TableName, contractTable)
		}
		if put.ConditionExpression == nil || *put.ConditionExpression != "attribute_not_exists(PK)" ||
			len(put.ExpressionAttributeNames) != 0 || len(put.ExpressionAttributeValues) != 0 {
			rt.Fatalf("PutItem condition = %v, names %v, values %v, want the bare create condition",
				put.ConditionExpression, put.ExpressionAttributeNames, put.ExpressionAttributeValues)
		}

		// One vault item and one event item, in separate partitions.
		if n := env.fake.itemCount(); n != 2 {
			rt.Fatalf("itemCount = %d, want 2 (one vault, one event)", n)
		}
		if n := len(env.fake.rawItemsUnder(pk)); n != 1 {
			rt.Fatalf("%d items under %q, want 1", n, pk)
		}
		if n := len(env.fake.rawItemsUnder("VAULT#" + payer)); n != 1 {
			rt.Fatalf("%d items under the vault partition, want 1", n)
		}
		contractWantRecord(rt, env.vs, vault.ID, vault)
		historyWantRead(rt, hs, payer, 10, []HistoryEvent{ev})

		// The read itself: one Query on that partition and nothing else.
		q := env.fake.lastQuery
		wantPK := &types.AttributeValueMemberS{Value: pk}
		if got := q.ExpressionAttributeValues[":pk"]; !reflect.DeepEqual(got, wantPK) {
			rt.Fatalf("Query :pk = %v, want %v", got, wantPK)
		}
		queriesBefore := env.fake.queryCalls
		if _, err := hs.RecentHistory(ctx, payer, 0); err != nil || env.fake.queryCalls != queriesBefore {
			rt.Fatalf("RecentHistory(limit 0) = %v and %d Query calls, want nil and no new Query (%d)",
				err, env.fake.queryCalls, queriesBefore)
		}
	})
}

// historyEchoing returns an error whose text repeats every value of an event and
// of the payer, the way an SDK error can echo the request that failed.
func historyEchoing(payer string, ev HistoryEvent) error {
	return fmt.Errorf("request failed for payer %s id %s at %d amount %d ref %s expires %d",
		payer, ev.ID, ev.At, ev.AmountINR, ev.BeneficiaryRef, ev.ExpiresAt)
}

// Feature: risk-intelligence, Property 14: The history store contract
//
// A write or a read on a cancelled context returns an error that matches
// context.Canceled, returns no events, changes nothing and, for DynamoDBStore,
// sends no request.
//
// **Validates: Requirements 13.13, 24.5**
func TestHistoryCancelledContextDoesNothing(t *testing.T) {
	contractCheck(t, func(rt *rapid.T, env *contractEnv) {
		hs := historyOf(rt, env)
		stored := historyEventGen().Draw(rt, "stored")
		stored.ExpiresAt = 0
		historyMustAppend(rt, hs, PayerKey, stored)
		before := historyCallsOf(env)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		ev := historyEventGen().Draw(rt, "event")
		if err := hs.AppendHistory(ctx, PayerKey, ev); !errors.Is(err, context.Canceled) {
			rt.Fatalf("AppendHistory on a cancelled context = %v, want context.Canceled", err)
		}
		limit := historyLimitGen().Draw(rt, "limit")
		got, err := hs.RecentHistory(ctx, PayerKey, limit)
		if !errors.Is(err, context.Canceled) || got != nil {
			rt.Fatalf("RecentHistory(limit %d) on a cancelled context = %v, %v, want nil and context.Canceled", limit, got, err)
		}
		if after := historyCallsOf(env); after != before {
			rt.Fatalf("client calls after cancelled methods = %+v, want %+v", after, before)
		}
		historyWantRead(rt, hs, PayerKey, 10, []HistoryEvent{stored})
	})
}

// Feature: risk-intelligence, Property 14: The history store contract
//
// DynamoDBStore only: a client failure on a write or a read, and an item that
// cannot be decoded, return an error that keeps the cause reachable and prints
// only the operation, the event id and a fixed description, even when the cause
// repeats every value of the event. A failed call returns no events and changes
// no stored event.
//
// **Validates: Requirements 13.13, 24.5**
func TestHistoryDynamoDBFailuresHoldNoEventValues(t *testing.T) {
	contractCheckDynamo(t, func(rt *rapid.T, env *contractEnv) {
		ctx := context.Background()
		hs := historyOf(rt, env)

		stored := historyEventGen().Draw(rt, "stored")
		stored.ExpiresAt = 0
		ev := historyEventGen().Draw(rt, "event")
		ev.ID = contractIDGen().Draw(rt, "id")
		ev.ExpiresAt = 0
		injected := historyEchoing(PayerKey, ev)
		historyMustAppend(rt, hs, PayerKey, stored)

		// The write fails.
		env.fake.failNext(fakeDynamoDBOpPutItem, injected)
		err := hs.AppendHistory(ctx, PayerKey, ev)
		if !errors.Is(err, injected) || err.Error() != "append_history "+ev.ID+": dynamodb request failed" {
			rt.Fatalf("a failed write returned %q, want the fixed text wrapping the cause", err)
		}

		// The read fails.
		env.fake.failNext(fakeDynamoDBOpQuery, injected)
		got, err := hs.RecentHistory(ctx, PayerKey, 10)
		if !errors.Is(err, injected) || err.Error() != "recent_history -: dynamodb request failed" || got != nil {
			rt.Fatalf("a failed read returned %v, %q, want nil events and the fixed text wrapping the cause", got, err)
		}

		// Nothing changed: the failed write stored nothing.
		historyWantRead(rt, hs, PayerKey, 10, []HistoryEvent{stored})

		// An item that cannot be decoded fails the whole read.
		bad := fakeDynamoDBHistoryItem(PayerKey, historySortKey(math.MaxInt64, "x"), "")
		bad["at"] = &types.AttributeValueMemberS{Value: "not a number"}
		if _, err := env.fake.PutItem(ctx, &dynamodb.PutItemInput{Item: bad}); err != nil {
			rt.Fatalf("seed an undecodable item: %v", err)
		}
		got, err = hs.RecentHistory(ctx, PayerKey, 10)
		if err == nil || err.Error() != "recent_history -: dynamodb item decode failed" || got != nil {
			rt.Fatalf("a read with an undecodable item returned %v, %v, want nil events and the fixed decode text", got, err)
		}
	})
}
