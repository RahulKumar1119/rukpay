package risk

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// This file holds the independent reference models for the behavioural rules of
// Requirements 10 and 11 and for the caps of Requirement 1 criteria 5 and 6. It
// is written from the requirement text. It shares no code, no table and no
// constant with the risk package: it has its own windows, its own tier bands, its own
// pattern row, its own cap numbers, and it does every comparison that could
// overflow an int64 with math/big. The only things it takes from the package
// are the input types, because those are the data under test, and refEntry,
// refBand and the baseline model of reference_test.go, which are test code.

// Windows of Requirement 10, in seconds, as the requirement text states them.
const (
	refVelocityWindow  = 600
	refFrequencyWindow = 86_400
	refRepeatWindow    = 3_600
)

// Bands of Requirement 10 criteria 1 to 3. Counts are never negative, so each
// table starts at 0.
var (
	// Criterion 1: 0 points for 0 to 2 events, 5 for 3 or 4, 10 for 5 to 7 and
	// 15 for 8 or more.
	refVelocityBands = []refBand{{0, 2, 0}, {3, 4, 5}, {5, 7, 10}, {8, math.MaxInt64, 15}}
	// Criterion 2: 0 points for 0 to 9 events, 5 for 10 to 19 and 10 for 20 or
	// more.
	refDailyBands = []refBand{{0, 9, 0}, {10, 19, 5}, {20, math.MaxInt64, 10}}
	// Criterion 3: 0 points for 0 or 1 event, 5 for 2 or 3 and 10 for 4 or more.
	refRepeatBands = []refBand{{0, 1, 0}, {2, 3, 5}, {4, math.MaxInt64, 10}}
)

// refWithin is the Within_Window definition of the glossary, computed with
// arbitrary-precision integers: an event is within a window of w seconds when
// the evaluation time minus the event time is less than w, and an event time
// later than the evaluation time is within every window.
func refWithin(now, at, w int64) bool {
	age := new(big.Int).Sub(big.NewInt(now), big.NewInt(at))
	if age.Sign() < 0 {
		return true
	}
	return age.Cmp(big.NewInt(w)) < 0
}

// refCount returns how many events of the history satisfy keep.
func refCount(history []HistoryEvent, keep func(HistoryEvent) bool) int64 {
	var n int64
	for _, e := range history {
		if keep(e) {
			n++
		}
	}
	return n
}

// refCounts holds the three counts that Requirement 10 reads.
type refCounts struct {
	velocity, daily, repeat int64
}

// refWindowCounts returns the three counts of Requirement 10 for the input. The
// repeat count is 0 for an empty Beneficiary_Ref, which is never matched to an
// empty ref (criterion 4), and otherwise counts events with exactly equal ref
// text (criterion 3).
func refWindowCounts(in Input) refCounts {
	return refCounts{
		velocity: refCount(in.History, func(e HistoryEvent) bool {
			return refWithin(in.Now, e.At, refVelocityWindow)
		}),
		daily: refCount(in.History, func(e HistoryEvent) bool {
			return refWithin(in.Now, e.At, refFrequencyWindow)
		}),
		repeat: refCount(in.History, func(e HistoryEvent) bool {
			return in.BeneficiaryRef != "" && e.BeneficiaryRef == in.BeneficiaryRef &&
				refWithin(in.Now, e.At, refRepeatWindow)
		}),
	}
}

// refWindowEntries returns the velocity, daily_frequency and repeat_attempts
// entries that the model expects, in that order, with a Signal_Id left out when
// it earns 0 points.
func refWindowEntries(in Input) []refEntry {
	c := refWindowCounts(in)
	candidates := []refEntry{
		{"velocity", refBandPoints(refVelocityBands, c.velocity)},
		{"daily_frequency", refBandPoints(refDailyBands, c.daily)},
		{"repeat_attempts", refBandPoints(refRepeatBands, c.repeat)},
	}
	var out []refEntry
	for _, e := range candidates {
		if e.Points > 0 {
			out = append(out, e)
		}
	}
	return out
}

// refDeviationPoints is Requirement 11 criterion 1 and 2. It holds only for
// event amounts of 1 or more, which is the domain of the requirement. The
// comparison "amount_inr is at least k times the largest amount" multiplies in
// arbitrary precision, so it cannot overflow.
func refDeviationPoints(amount int64, history []HistoryEvent) int {
	if len(history) < 5 || amount < 2000 {
		return 0
	}
	largest := big.NewInt(history[0].AmountINR)
	for _, e := range history {
		if v := big.NewInt(e.AmountINR); v.Cmp(largest) > 0 {
			largest = v
		}
	}
	a := big.NewInt(amount)
	if a.Cmp(new(big.Int).Mul(big.NewInt(10), largest)) >= 0 {
		return 15
	}
	if a.Cmp(new(big.Int).Mul(big.NewInt(3), largest)) >= 0 {
		return 10
	}
	return 0
}

// refPattern is one Fraud_Pattern as Requirement 11 criterion 3 describes it.
type refPattern struct {
	ID       string
	Window   int64
	MinSmall int
	SmallMax int64
	LargeMin int64
	Points   int
}

// refPatternTable is the Pattern_Table as Requirement 11 criterion 4 states it:
// the entry test_then_large with a window of 3600 seconds, a minimum of 3 small
// payments, a small-payment maximum of 1000 rupees, a large-payment minimum of
// 10000 rupees and 15 points.
var refPatternTable = []refPattern{
	{ID: "test_then_large", Window: 3_600, MinSmall: 3, SmallMax: 1_000, LargeMin: 10_000, Points: 15},
}

// refPatternEntries returns the pattern entries the model expects for table, in
// table order, as Requirement 11 criteria 5, 6 and 8 describe them: a payment
// with a non-empty ref whose amount is at least the large minimum earns the
// points of an entry once when the snapshot holds at least the minimum number
// of events that have the same ref, are within the window and are no larger
// than the small maximum.
func refPatternEntries(table []refPattern, in Input) []refEntry {
	var out []refEntry
	for _, p := range table {
		if in.BeneficiaryRef == "" || in.Signals.AmountINR < p.LargeMin {
			continue
		}
		small := refCount(in.History, func(e HistoryEvent) bool {
			return e.BeneficiaryRef == in.BeneficiaryRef &&
				refWithin(in.Now, e.At, p.Window) &&
				e.AmountINR <= p.SmallMax
		})
		if small >= int64(p.MinSmall) {
			out = append(out, refEntry{Signal: "pattern_" + p.ID, Points: p.Points})
		}
	}
	return out
}

// refBehaviourEntries returns the behavioural entries in the order of
// Requirement 1 criterion 3: velocity, daily_frequency, repeat_attempts,
// amount_deviation, then the patterns in table order.
func refBehaviourEntries(table []refPattern, in Input) []refEntry {
	out := refWindowEntries(in)
	if p := refDeviationPoints(in.Signals.AmountINR, in.History); p > 0 {
		out = append(out, refEntry{Signal: "amount_deviation", Points: p})
	}
	return append(out, refPatternEntries(table, in)...)
}

// refAssessment is what the model expects of a whole evaluation, with the caps
// of Requirement 1 criteria 5 and 6 and Requirement 9 criteria 1 and 2.
type refAssessment struct {
	// Entries are all expected Contributions, cap lines included.
	Entries []refEntry
	// BehaviourSum is the behavioural sum before the cap.
	BehaviourSum int
	// Behaviour is the behavioural sum after the cap.
	Behaviour int
	Baseline  int
	Score     int
	State     store.VaultState
}

// refAssess is the model of a whole evaluation that names no Beneficiary: the
// baseline entries of reference_test.go, the behavioural entries, a
// behaviour_cap entry only when the behavioural sum is above 25, and a
// score_cap entry only when the capped parts add up to more than 100.
func refAssess(table []refPattern, in Input) refAssessment {
	baseline := refBaseline(in.Signals)
	behaviour := refBehaviourEntries(table, in)

	r := refAssessment{
		Baseline:     refBaselineTotal(baseline),
		BehaviourSum: refBaselineTotal(behaviour),
	}
	r.Entries = append(append([]refEntry{}, baseline...), behaviour...)

	r.Behaviour = r.BehaviourSum
	if r.BehaviourSum > 25 {
		r.Entries = append(r.Entries, refEntry{Signal: "behaviour_cap", Points: 25 - r.BehaviourSum})
		r.Behaviour = 25
	}
	total := r.Baseline + r.Behaviour
	r.Score = total
	if total > 100 {
		r.Entries = append(r.Entries, refEntry{Signal: "score_cap", Points: 100 - total})
		r.Score = 100
	}
	r.State = frozenMapState(r.Score)
	return r
}

// refReasons holds the sentence of every tier, written from the sentence table
// of the design and not read from sentences.go. A Contribution is looked up by
// its Signal_Id and points, except for the two cap lines, which have one
// sentence whatever their points.
var refReasons = map[string]string{
	"amount/10":                  "This payment is bigger than a small everyday purchase.",
	"amount/20":                  "This is a large payment.",
	"amount/35":                  "This is a very large payment.",
	"beneficiary_age/25":         "The recipient was added less than a day ago.",
	"beneficiary_age/10":         "The recipient was added in the last few days.",
	"late_night/10":              "The payment is being made late at night.",
	"unknown_call/20":            "A call from someone you do not know is going on.",
	"rapid_retries/5":            "The payment has been tried a couple of times in a row.",
	"rapid_retries/10":           "The payment has been tried several times in a row.",
	"velocity/5":                 "Several payments were made in the last few minutes.",
	"velocity/10":                "Many payments were made in the last few minutes.",
	"velocity/15":                "A lot of payments were made in the last few minutes.",
	"daily_frequency/5":          "There have been quite a few payments in the last day.",
	"daily_frequency/10":         "There have been a lot of payments in the last day.",
	"repeat_attempts/5":          "A few payments went to this same recipient in the last hour.",
	"repeat_attempts/10":         "Many payments went to this same recipient in the last hour.",
	"amount_deviation/10":        "This payment is much larger than your recent ones.",
	"amount_deviation/15":        "This payment is far larger than your recent ones.",
	"pattern_test_then_large/15": "Several small payments to this recipient were followed by a large one.",
	"behaviour_cap":              "The extra points from recent activity have an upper limit.",
	"score_cap":                  "The total cannot go above its upper limit.",
}

// refReason returns the sentence that the model gives a Contribution, and false
// when the model has none for that Signal_Id and points.
func refReason(signal string, points int) (string, bool) {
	if signal == "behaviour_cap" || signal == "score_cap" {
		r, ok := refReasons[signal]
		return r, ok
	}
	r, ok := refReasons[signal+"/"+strconv.Itoa(points)]
	return r, ok
}

// refReasonProblem returns a text for the first Contribution whose reason is not
// the sentence of its Signal_Id and tier, or "" when every one is. A pattern
// Contribution that the model has no sentence for is skipped when
// skipUnknownPatterns is set, for a test that adds a pattern row of its own.
func refReasonProblem(explanation []Contribution, skipUnknownPatterns bool) string {
	for _, c := range explanation {
		want, ok := refReason(c.Signal, c.Points)
		if !ok {
			if skipUnknownPatterns && strings.HasPrefix(c.Signal, "pattern_") {
				continue
			}
			return fmt.Sprintf("the model has no sentence for %q with %d points", c.Signal, c.Points)
		}
		if c.Reason != want {
			return fmt.Sprintf("%q with %d points has the reason %q, want %q", c.Signal, c.Points, c.Reason, want)
		}
	}
	return ""
}
