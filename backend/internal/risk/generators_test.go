package risk

import (
	"math"
	"sort"
	"strings"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// This file holds the generators that the Assess property tests share. Every
// Assess property draws its input from genInput (or genInvalidInput) and reads
// the answer through the abstractions in this file and in reference_test.go.
// Group 8 widened Input with a History_Snapshot, an evaluation time and a
// Beneficiary_Ref, and widened genInput, genInvalidInput, cloneInput and
// baselineOnly here, so the property tests needed no edit. Group 12 widened them
// again with the recipient data. genInput draws only recipients that earn no
// care points, see genRecipientWithoutCare for why, and genInvalidInput draws
// every kind.

// genAmount draws an amount_inr greater than 0. It mixes every tier boundary,
// the smallest and the largest valid amount, small ordinary amounts and the
// whole valid range.
func genAmount() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{1, 1999, 2000, 9999, 10000, 24999, 25000, math.MaxInt64}),
		rapid.Int64Range(1, 30000),
		rapid.Int64Range(1, math.MaxInt64),
	)
}

// genAgeHours draws a beneficiary_age_hours of 0 or more. It mixes every tier
// boundary, the largest age, small ordinary ages and the whole valid range.
func genAgeHours() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{0, 1, 23, 24, 71, 72, 73, math.MaxInt64}),
		rapid.Int64Range(0, 100),
		rapid.Int64Range(0, math.MaxInt64),
	)
}

// genRetries draws a rapid_retry_count of 0 or more. It mixes every tier
// boundary, a very large count, small ordinary counts and the whole valid
// range.
func genRetries() *rapid.Generator[int] {
	return rapid.OneOf(
		rapid.SampledFrom([]int{0, 1, 2, 3, 4, math.MaxInt}),
		rapid.IntRange(0, 10),
		rapid.IntRange(0, math.MaxInt),
	)
}

// genSignals draws valid Signals that reach every tier boundary of the baseline
// rules: amount 1999 and 2000, 9999 and 10000, 24999 and 25000, age 23 and 24
// and 71 and 72, retries 1 and 2 and 3, and the largest amount.
func genSignals() *rapid.Generator[Signals] {
	return rapid.Custom(func(t *rapid.T) Signals {
		return Signals{
			AmountINR:         genAmount().Draw(t, "amount_inr"),
			BeneficiaryAgeHrs: genAgeHours().Draw(t, "beneficiary_age_hours"),
			IsLateNight:       rapid.Bool().Draw(t, "is_late_night"),
			ActiveUnknownCall: rapid.Bool().Draw(t, "active_unknown_call"),
			RapidRetryCount:   genRetries().Draw(t, "rapid_retry_count"),
		}
	})
}

// genRecipientWithoutCare draws the recipient data of a payment that earns no
// care points: nil for a payment that names no saved recipient, or a Recipient on
// STANDARD protection with any verified time and any Pressure_Flag.
//
// genInput uses it, and not genRecipient, for one reason. Two Test functions
// written before group 12 break when the score can include care points:
//
//   - TestLowBaselineWithNoExtraPointsStaysInstantPass (Property 5) tests for
//     "no extra points" through Behaviour alone, so a baseline of 35 with 10 care
//     points gives 45 and fails it;
//   - TestAssessMatchesTheBehaviourReferenceModel (Property 1) compares the whole
//     Explanation with refAssess, which has no care line.
//
// Neither Test function may be edited by group 12, so the inputs that earn care
// points come from genRecipient through genCareInput in pbt_care_test.go, which
// also holds the care-bearing versions of the checks of Properties 1, 3, 4 and 5.
// When those two tests are updated (the first to look at Care, the second to use
// a care-aware model), change the recipient draw in genInput to genRecipient and
// the other tests need no edit.
func genRecipientWithoutCare() *rapid.Generator[*Recipient] {
	return rapid.Custom(func(t *rapid.T) *Recipient {
		if rapid.Bool().Draw(t, "no_recipient") {
			return nil
		}
		return &Recipient{
			Protection: store.ProtectionStandard,
			Verified:   rapid.Bool().Draw(t, "standard_verified"),
			Pressure:   rapid.Bool().Draw(t, "standard_pressure"),
		}
	})
}

// genRecipient draws the recipient data of a payment: nil for a payment that
// names no saved recipient, or a Recipient on STANDARD or EXTRA_CARE protection
// with every combination of the verified time and the Pressure_Flag. The three
// care cases (0, 5 and 10 points) each come up in about a quarter of the draws.
func genRecipient() *rapid.Generator[*Recipient] {
	return rapid.Custom(func(t *rapid.T) *Recipient {
		// rapid favours small numbers, so the kinds that earn points come first.
		switch rapid.IntRange(0, 11).Draw(t, "recipient_kind") {
		case 0, 1:
			// EXTRA_CARE with no verified time: 10 points, with or without the flag.
			return &Recipient{
				Protection: store.ProtectionExtraCare,
				Pressure:   rapid.Bool().Draw(t, "unverified_pressure"),
			}
		case 2, 3:
			// EXTRA_CARE with a verified time and the flag: 10 points.
			return &Recipient{Protection: store.ProtectionExtraCare, Verified: true, Pressure: true}
		case 4, 5, 6:
			// EXTRA_CARE with a verified time and no flag: 5 points.
			return &Recipient{Protection: store.ProtectionExtraCare, Verified: true}
		case 7, 8, 9:
			// STANDARD earns 0 whatever the verified time and the flag say.
			return &Recipient{
				Protection: store.ProtectionStandard,
				Verified:   rapid.Bool().Draw(t, "standard_verified"),
				Pressure:   rapid.Bool().Draw(t, "standard_pressure"),
			}
		default:
			return nil
		}
	})
}

// genInput draws a valid Input. It is the one generator that every Assess
// property test calls.
//
// The History_Snapshot is drawn in one of five ways, so that the rare cases that
// matter come up often: no events, scattered events, a burst of events that is
// all inside one window, a history built around the amount deviation boundary,
// and a history built around the test_then_large pattern. One input in five has
// a heavy baseline, so the total goes over 100 often enough to reach the
// score_cap line.
func genInput() *rapid.Generator[Input] {
	return rapid.Custom(func(t *rapid.T) Input {
		signals := genSignals().Draw(t, "signals")
		mode := genHistoryMode().Draw(t, "history_mode")
		now := genNow().Draw(t, "now")
		ref := genPaymentRef().Draw(t, "beneficiary_ref")

		if rapid.IntRange(0, 4).Draw(t, "heavy_baseline") == 0 {
			signals = genHeavySignals().Draw(t, "heavy_signals")
		}
		// The pattern needs a large payment, so most pattern histories get one,
		// and half of them one next to the large-payment minimum of 10000.
		if mode == historyPattern {
			switch pick := rapid.IntRange(0, 9).Draw(t, "large_payment"); {
			case pick < 5:
				signals.AmountINR = rapid.SampledFrom([]int64{9999, 10000, 10000, 10001}).Draw(t, "edge_amount")
			case pick < 8:
				signals.AmountINR = genLargeAmount().Draw(t, "large_amount")
			}
		}
		history := drawHistory(t, mode, signals.AmountINR, now, ref)
		// One input in twenty has the largest amount as the payment and as one
		// of the events (Requirement 27 criterion 3).
		if len(history) > 0 && rapid.IntRange(0, 19).Draw(t, "largest_amounts") == 0 {
			signals.AmountINR = math.MaxInt64
			history[rapid.IntRange(0, len(history)-1).Draw(t, "largest_event")].AmountINR = math.MaxInt64
		}
		return Input{
			Signals:        signals,
			Now:            now,
			History:        history,
			BeneficiaryRef: ref,
			Recipient:      genRecipientWithoutCare().Draw(t, "recipient"),
		}
	})
}

// genInputFor draws an Input for the given Signals with any evaluation time,
// Beneficiary_Ref and History_Snapshot. A property that fixes the Signals, such
// as the Preset guarantees, uses it to vary everything else.
func genInputFor(signals Signals) *rapid.Generator[Input] {
	return rapid.Custom(func(t *rapid.T) Input {
		mode := genHistoryMode().Draw(t, "history_mode")
		now := genNow().Draw(t, "now")
		ref := genPaymentRef().Draw(t, "beneficiary_ref")
		return Input{
			Signals:        signals,
			Now:            now,
			History:        drawHistory(t, mode, signals.AmountINR, now, ref),
			BeneficiaryRef: ref,
		}
	})
}

// historyMode names how drawHistory builds a History_Snapshot.
type historyMode int

const (
	// historyEmpty draws no event, as nil or as an empty list.
	historyEmpty historyMode = iota
	// historyScatter draws up to 100 events with free times, refs and amounts.
	historyScatter
	// historyBurst draws a count of events that all fall inside one window,
	// with a few just outside it.
	historyBurst
	// historyDeviation draws 0 to 20 events whose largest amount sits at a
	// boundary of the amount deviation rule.
	historyDeviation
	// historyPattern draws small payments to the payment's recipient around the
	// edges of the test_then_large pattern, with noise events beside them.
	historyPattern
)

// genHistoryMode draws a historyMode. The empty mode is one in twelve, and other
// modes also draw no event now and then, so the frozen Evaluate comparison of
// Property 4 keeps getting inputs.
func genHistoryMode() *rapid.Generator[historyMode] {
	return rapid.SampledFrom([]historyMode{
		historyEmpty,
		historyScatter, historyScatter,
		historyBurst, historyBurst, historyBurst, historyBurst, historyBurst,
		historyDeviation, historyDeviation,
		historyPattern, historyPattern,
	})
}

// genNow draws an evaluation time in epoch seconds. Most are ordinary times,
// so that the window boundaries are exact. Some are the extremes of int64 and
// some are drawn from the whole range, because the window rule must hold for
// any int64.
func genNow() *rapid.Generator[int64] {
	typical := rapid.Int64Range(1_700_000_000, 1_900_000_000)
	return rapid.OneOf(
		typical, typical, typical, typical,
		rapid.SampledFrom([]int64{0, 1, 599, 600, 604_800, math.MaxInt64, math.MaxInt64 - 1, math.MinInt64, math.MinInt64 + 1}),
		rapid.Int64Range(0, math.MaxInt64),
		rapid.Int64(),
	)
}

// genPaymentRef draws the Beneficiary_Ref of the payment: empty for a payment
// with no recipient, or one masked identifier of each format.
func genPaymentRef() *rapid.Generator[string] {
	return rapid.SampledFrom([]string{
		"", "",
		"r***@okaxis", "r***@okaxis",
		"XXXX-XXXX-4821",
		"+91-XXXXXX3210",
	})
}

// genLargeAmount draws an amount of 10000 or more, around the large-payment
// minimum of the test_then_large pattern and the 3 times and 10 times marks.
func genLargeAmount() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{9999, 10000, 10001, 24999, 25000, 85000, math.MaxInt64}),
		rapid.Int64Range(10000, 100000),
		rapid.Int64Range(10000, math.MaxInt64),
	)
}

// genHeavySignals draws valid Signals whose baseline is 80 or more: an amount of
// 25000 or more, a beneficiary age below 24 hours and an unknown call. The
// other two signals are free, so the baseline is anywhere from 80 to 100.
func genHeavySignals() *rapid.Generator[Signals] {
	return rapid.Custom(func(t *rapid.T) Signals {
		return Signals{
			AmountINR: rapid.OneOf(
				rapid.SampledFrom([]int64{25000, 25001, 85000, math.MaxInt64}),
				rapid.Int64Range(25000, math.MaxInt64),
			).Draw(t, "amount_inr"),
			BeneficiaryAgeHrs: rapid.Int64Range(0, 23).Draw(t, "beneficiary_age_hours"),
			IsLateNight:       rapid.Bool().Draw(t, "is_late_night"),
			ActiveUnknownCall: true,
			RapidRetryCount:   genRetries().Draw(t, "rapid_retry_count"),
		}
	})
}

// timeBefore returns the time that is age seconds before now, for age 0 or
// more. It stops at the smallest int64 instead of wrapping.
func timeBefore(now, age int64) int64 {
	if now < math.MinInt64+age {
		return math.MinInt64
	}
	return now - age
}

// timeAfter returns the time that is d seconds after now, for d 0 or more. It
// stops at the largest int64 instead of wrapping.
func timeAfter(now, d int64) int64 {
	if now > math.MaxInt64-d {
		return math.MaxInt64
	}
	return now + d
}

// boundaryAges are event ages in seconds that sit on or next to the edge of a
// window: the velocity window (600), the repeat window and the pattern window
// (3600), the frequency window (86400) and the retention (604800).
var boundaryAges = []int64{
	0, 1, 2, 59,
	598, 599, 600, 601, 602,
	3598, 3599, 3600, 3601, 3602,
	86398, 86399, 86400, 86401,
	604_798, 604_799, 604_800, 604_801,
}

// genAge draws an event age in seconds: a boundary age, an ordinary age or a
// very large age.
func genAge() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom(boundaryAges),
		rapid.SampledFrom(boundaryAges),
		rapid.Int64Range(0, 700),
		rapid.Int64Range(0, 700_000),
		rapid.Just(int64(math.MaxInt64)),
	)
}

// genEventAmount draws the amount of a History_Event: 1 or more, because the
// service records only evaluations that passed validation. It mixes the edges of
// the small and large amounts of the pattern, the deviation floor and the
// largest amount.
func genEventAmount() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{1, 999, 1000, 1001, 1999, 2000, 2001, 9999, 10000, 10001, math.MaxInt64, math.MaxInt64 - 1}),
		rapid.Int64Range(1, 1500),
		rapid.Int64Range(1, 30000),
		rapid.Int64Range(1, math.MaxInt64),
	)
}

// genSmallAmount draws an amount for a small payment of the pattern: 1 up to a
// little over the small-payment maximum of 1000.
func genSmallAmount() *rapid.Generator[int64] {
	return rapid.OneOf(
		rapid.SampledFrom([]int64{1, 500, 999, 1000, 1000, 1001, 1002}),
		rapid.Int64Range(1, 1000),
	)
}

// drawEventRef draws the Beneficiary_Ref of an event for a payment with the
// given ref. Most match the payment, some are empty, some are other
// identifiers, and some are the payment's ref with its case or its surrounding
// space changed, which must never match.
func drawEventRef(t *rapid.T, ref string) string {
	return rapid.SampledFrom([]string{
		ref, ref, ref, ref,
		"", "",
		"r***@okaxis", "XXXX-XXXX-4821", "+91-XXXXXX3210",
		strings.ToUpper(ref), ref + " ", " " + ref,
	}).Draw(t, "event_ref")
}

// drawEventTime draws the time of an event. It is usually in the past by an age
// from genAge, sometimes in the future, and sometimes equal to the time of an
// event drawn earlier, so that runs of events with equal times exist.
func drawEventTime(t *rapid.T, now int64, prior []HistoryEvent) int64 {
	if len(prior) > 0 && rapid.IntRange(0, 4).Draw(t, "equal_time") == 0 {
		return prior[rapid.IntRange(0, len(prior)-1).Draw(t, "equal_time_index")].At
	}
	if rapid.IntRange(0, 11).Draw(t, "future") == 0 {
		d := rapid.OneOf(
			rapid.SampledFrom([]int64{1, 2, 3, 600, 3600, 86400}),
			rapid.Int64Range(1, 1_000_000),
		).Draw(t, "future_seconds")
		return timeAfter(now, d)
	}
	// Some events have a time unrelated to now, from the whole int64 range, so
	// that now minus the event time sometimes does not fit in an int64.
	if rapid.IntRange(0, 15).Draw(t, "any_time") == 0 {
		return rapid.OneOf(
			rapid.Int64(),
			rapid.SampledFrom([]int64{math.MinInt64, math.MinInt64 + 1, -1, 0, math.MaxInt64 - 1, math.MaxInt64}),
		).Draw(t, "any_time_value")
	}
	return timeBefore(now, genAge().Draw(t, "age"))
}

// drawEvent draws one History_Event with free time, ref and amount.
func drawEvent(t *rapid.T, now int64, ref string, prior []HistoryEvent) HistoryEvent {
	return HistoryEvent{
		At:             drawEventTime(t, now, prior),
		AmountINR:      genEventAmount().Draw(t, "event_amount"),
		BeneficiaryRef: drawEventRef(t, ref),
	}
}

// drawLargest draws the largest amount of a deviation history so that the
// payment amount lands on or next to the 3 times and 10 times marks.
func drawLargest(t *rapid.T, amount int64) int64 {
	if amount < 1 {
		amount = 1
	}
	atLeastOne := func(v int64) int64 {
		if v < 1 {
			return 1
		}
		return v
	}
	a10, a3 := atLeastOne(amount/10), atLeastOne(amount/3)
	// For an amount of 3L-1 or 10L-1 the largest is (amount+1)/3 or
	// (amount+1)/10, so those two are drawn as well. The sum cannot overflow
	// because the amount is first lowered by 1 when it is the largest int64.
	below := amount
	if below == math.MaxInt64 {
		below--
	}
	return rapid.SampledFrom([]int64{
		a10, a10 + 1, atLeastOne(a10 - 1), atLeastOne((below + 1) / 10),
		a3, a3 + 1, atLeastOne(a3 - 1), atLeastOne((below + 1) / 3),
		1, 666, 1000, 1001, math.MaxInt64,
	}).Draw(t, "largest")
}

// drawHistory draws a History_Snapshot of 0 to 100 events in mode, oldest
// first. amount is the amount of the payment and ref its Beneficiary_Ref, so
// that the deviation and pattern modes can aim at them.
func drawHistory(t *rapid.T, mode historyMode, amount, now int64, ref string) []HistoryEvent {
	var events []HistoryEvent
	switch mode {
	case historyEmpty:
		if rapid.Bool().Draw(t, "nil_history") {
			return nil
		}
		return []HistoryEvent{}

	case historyScatter:
		n := rapid.OneOf(rapid.IntRange(0, 8), rapid.IntRange(0, 100)).Draw(t, "scatter_count")
		for i := 0; i < n; i++ {
			events = append(events, drawEvent(t, now, ref, events))
		}

	case historyBurst:
		// A burst aims at the edges of one tier table: velocity counts 2 3 4 5 7
		// 8 in 600 seconds, daily counts 9 10 19 20 in 86400 seconds and repeat
		// counts 1 2 3 4 in 3600 seconds. A free burst draws any count from 0
		// to 100 in any of the three windows.
		var span int64
		var k int
		switch rapid.SampledFrom([]int{0, 0, 1, 1, 2, 2, 3}).Draw(t, "burst_family") {
		case 0:
			span = 600
			k = rapid.SampledFrom([]int{2, 3, 4, 5, 7, 8}).Draw(t, "velocity_edge")
		case 1:
			span = 86400
			k = rapid.SampledFrom([]int{9, 10, 19, 20}).Draw(t, "daily_edge")
		case 2:
			span = 3600
			k = rapid.SampledFrom([]int{1, 2, 3, 4}).Draw(t, "repeat_edge")
		default:
			span = rapid.SampledFrom([]int64{600, 3600, 86400}).Draw(t, "burst_span")
			k = rapid.IntRange(0, 100).Draw(t, "burst_count")
		}
		// Most bursts have no event outside the window, so the exact count
		// reaches the tier edge. The rest have a few at or just past it.
		strict := rapid.IntRange(0, 7).Draw(t, "burst_strict") != 0
		sameRef := rapid.IntRange(0, 9).Draw(t, "burst_same_ref") < 8
		for i := 0; i < k; i++ {
			age := rapid.OneOf(
				rapid.Int64Range(0, span-1),
				rapid.SampledFrom([]int64{0, span - 1, span - 1}),
			).Draw(t, "burst_age")
			if !strict && rapid.IntRange(0, 7).Draw(t, "burst_outside") == 0 {
				age = rapid.SampledFrom([]int64{span, span + 1}).Draw(t, "burst_outside_age")
			}
			e := HistoryEvent{
				At:        timeBefore(now, age),
				AmountINR: rapid.OneOf(genSmallAmount(), genEventAmount()).Draw(t, "burst_amount"),
			}
			if sameRef {
				e.BeneficiaryRef = ref
			} else {
				e.BeneficiaryRef = drawEventRef(t, ref)
			}
			events = append(events, e)
		}

	case historyDeviation:
		n := rapid.SampledFrom([]int{0, 1, 3, 4, 4, 5, 5, 5, 6, 6, 7, 8, 20}).Draw(t, "deviation_count")
		largest := drawLargest(t, amount)
		for i := 0; i < n; i++ {
			e := drawEvent(t, now, ref, events)
			e.AmountINR = rapid.Int64Range(1, largest).Draw(t, "deviation_amount")
			events = append(events, e)
		}
		if n > 0 {
			events[rapid.IntRange(0, n-1).Draw(t, "largest_index")].AmountINR = largest
		}

	case historyPattern:
		m := rapid.SampledFrom([]int{0, 1, 2, 2, 3, 3, 3, 3, 4, 5, 7}).Draw(t, "small_count")
		for i := 0; i < m; i++ {
			age := rapid.OneOf(
				rapid.Int64Range(0, 3599),
				rapid.Int64Range(0, 3599),
				rapid.Int64Range(0, 3599),
				rapid.SampledFrom([]int64{0, 1, 3598, 3599, 3599, 3600, 3601}),
			).Draw(t, "small_age")
			e := HistoryEvent{
				At:             timeBefore(now, age),
				AmountINR:      genSmallAmount().Draw(t, "small_amount"),
				BeneficiaryRef: ref,
			}
			if rapid.IntRange(0, 9).Draw(t, "small_other_ref") == 0 {
				e.BeneficiaryRef = drawEventRef(t, ref)
			}
			events = append(events, e)
		}
		noise := rapid.IntRange(0, 4).Draw(t, "noise_count")
		for i := 0; i < noise; i++ {
			events = append(events, drawEvent(t, now, ref, events))
		}
	}

	// A History_Snapshot is oldest first. The sort is stable, so events with
	// equal times keep the order they were drawn in.
	sort.SliceStable(events, func(i, j int) bool { return events[i].At < events[j].At })
	return events
}

// genInvalidSignals draws Signals with at least one invalid field: an amount
// below 1, a negative beneficiary age or a negative retry count. It breaks any
// non-empty subset of the three fields, so the first-failing-field order is
// exercised, and keeps the other fields valid.
func genInvalidSignals() *rapid.Generator[Signals] {
	return rapid.Custom(func(t *rapid.T) Signals {
		s := genSignals().Draw(t, "signals")
		// Bits 1, 2 and 4 select the amount, the age and the retry count.
		mask := rapid.IntRange(1, 7).Draw(t, "broken_fields")
		if mask&1 != 0 {
			s.AmountINR = rapid.OneOf(
				rapid.SampledFrom([]int64{0, -1, math.MinInt64}),
				rapid.Int64Range(math.MinInt64, 0),
			).Draw(t, "invalid_amount_inr")
		}
		if mask&2 != 0 {
			s.BeneficiaryAgeHrs = rapid.OneOf(
				rapid.SampledFrom([]int64{-1, math.MinInt64}),
				rapid.Int64Range(math.MinInt64, -1),
			).Draw(t, "invalid_beneficiary_age_hours")
		}
		if mask&4 != 0 {
			s.RapidRetryCount = rapid.OneOf(
				rapid.SampledFrom([]int{-1, math.MinInt}),
				rapid.IntRange(math.MinInt, -1),
			).Draw(t, "invalid_rapid_retry_count")
		}
		return s
	})
}

// genInvalidInput draws an Input whose Signals are invalid, with any evaluation
// time, Beneficiary_Ref and History_Snapshot, because the validation error must
// not depend on them (Requirement 9 criterion 13).
func genInvalidInput() *rapid.Generator[Input] {
	return rapid.Custom(func(t *rapid.T) Input {
		signals := genInvalidSignals().Draw(t, "signals")
		mode := genHistoryMode().Draw(t, "history_mode")
		now := genNow().Draw(t, "now")
		ref := genPaymentRef().Draw(t, "beneficiary_ref")
		return Input{
			Signals:        signals,
			Now:            now,
			History:        drawHistory(t, mode, signals.AmountINR, now, ref),
			BeneficiaryRef: ref,
			Recipient:      genRecipient().Draw(t, "recipient"),
		}
	})
}

// cloneInput returns a copy of in that shares no memory with it, so a test can
// check that Assess left its argument unchanged. It copies the History_Snapshot
// and keeps a nil list nil and an empty list empty. A group that adds a
// pointer to Input deep copies it here.
func cloneInput(in Input) Input {
	out := in
	if in.History != nil {
		out.History = make([]HistoryEvent, len(in.History))
		copy(out.History, in.History)
	}
	if in.Recipient != nil {
		r := *in.Recipient
		out.Recipient = &r
	}
	return out
}

// baselineOnly reports whether the scored evaluation has an empty
// History_Snapshot and names no saved recipient, which are the inputs for which
// Assess must give the score and state that the shipped Evaluate gave. The
// evaluation time and the Beneficiary_Ref do not matter without events.
func baselineOnly(in Input) bool {
	return len(in.History) == 0 && in.Recipient == nil
}
