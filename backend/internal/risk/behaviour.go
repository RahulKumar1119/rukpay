package risk

// This file holds the behavioural signals that read the History_Snapshot: the
// three window signals (Requirement 10) and the amount deviation
// (Requirement 11). The Fraud_Pattern signals are in patterns.go. Every
// function here is a pure function of its arguments: no clock, no I/O, no
// randomness, and nothing it is given is changed.

// within reports whether an event at time at is inside a window of w seconds
// that ends at now. An event is inside when now minus at is less than w, so an
// event exactly w seconds old is outside and one w-1 seconds old is inside. An
// event later than now counts as inside every window of 1 second or more
// (Requirement 10, the Within_Window definition of the glossary).
//
// The subtraction runs in uint64 after the at > now test. With at <= now the
// true difference is from 0 to 2^64-1, which fits in uint64, so no pair of
// int64 values can overflow it.
func within(at, now, w int64) bool {
	if at > now {
		return true
	}
	if w <= 0 {
		return false
	}
	return uint64(now)-uint64(at) < uint64(w)
}

// Tier tables for the three window signals. Highest row first: the first row
// whose min the count reaches wins, and a count below every row earns nothing.
// Each row holds its points and its sentence together. The tables are read
// only: nothing writes to them after start-up.
var (
	// velocityTiers counts events in the Velocity_Window, whatever their
	// amount or recipient (Requirement 10 criterion 1).
	velocityTiers = []tier{
		{min: 8, points: 15, sentence: sentVelocityHigh},
		{min: 5, points: 10, sentence: sentVelocityMid},
		{min: 3, points: 5, sentence: sentVelocityLow},
	}
	// dailyTiers counts events in the Frequency_Window, whatever their amount
	// or recipient (Requirement 10 criterion 2).
	dailyTiers = []tier{
		{min: 20, points: 10, sentence: sentDailyHigh},
		{min: 10, points: 5, sentence: sentDailyLow},
	}
	// repeatTiers counts events in the Repeat_Window that went to the same
	// recipient as this payment (Requirement 10 criterion 3).
	repeatTiers = []tier{
		{min: 4, points: 10, sentence: sentRepeatHigh},
		{min: 2, points: 5, sentence: sentRepeatLow},
	}
)

// tierContribution returns the Contribution of the first row of tiers whose min
// count reaches, and false when count is below every row.
func tierContribution(signal string, tiers []tier, count int64) (Contribution, bool) {
	for _, t := range tiers {
		if count >= t.min {
			return Contribution{Signal: signal, Points: t.points, Reason: t.sentence}, true
		}
	}
	return Contribution{}, false
}

// appendWindowSignals adds the velocity, daily_frequency and repeat_attempts
// Contributions, in that order. Each is added only when its points are above 0
// and takes its points from its own count alone.
//
// repeat_attempts counts only events whose BeneficiaryRef is exactly equal text
// to ref, with no case folding and no trimming, and only when ref is not empty:
// an empty ref is never matched to an empty ref (Requirement 10 criterion 4).
func appendWindowSignals(list []Contribution, now int64, ref string, history []HistoryEvent) []Contribution {
	var velocity, daily, repeat int64
	for _, e := range history {
		if within(e.At, now, VelocityWindow) {
			velocity++
		}
		if within(e.At, now, FrequencyWindow) {
			daily++
		}
		if ref != "" && e.BeneficiaryRef == ref && within(e.At, now, RepeatWindow) {
			repeat++
		}
	}
	if c, ok := tierContribution(SignalVelocity, velocityTiers, velocity); ok {
		list = append(list, c)
	}
	if c, ok := tierContribution(SignalDailyFrequency, dailyTiers, daily); ok {
		list = append(list, c)
	}
	if c, ok := tierContribution(SignalRepeatAttempts, repeatTiers, repeat); ok {
		list = append(list, c)
	}
	return list
}

// deviationTier is one row of the amount deviation table: a payment that is at
// least factor times the largest recent payment earns points.
type deviationTier struct {
	factor   int64
	points   int
	sentence string
}

// deviationTiers lists the rows, highest factor first (Requirement 11
// criterion 1). Read only.
var deviationTiers = []deviationTier{
	{factor: 10, points: 15, sentence: sentDeviationHigh},
	{factor: 3, points: 10, sentence: sentDeviationMid},
}

// appendDeviation adds the amount_deviation Contribution when the snapshot holds
// at least DeviationMinEvents events and the amount is at least
// DeviationMinAmount rupees. It compares the amount with the largest amount of
// every event of the snapshot, whatever its recipient or age.
//
// The comparison "amount is at least factor times largest" is written as
// amount/factor >= largest, so that no multiplication can overflow int64. For a
// whole factor above 0 and a whole largest, the two are equal: if amount is at
// least factor*largest then amount/factor is at least largest, and if
// amount/factor is at least largest then amount is at least
// factor*(amount/factor), which is at least factor*largest. A snapshot with no
// amount above 0 earns nothing.
func appendDeviation(list []Contribution, amount int64, history []HistoryEvent) []Contribution {
	if len(history) < DeviationMinEvents || amount < DeviationMinAmount {
		return list
	}
	var largest int64
	for _, e := range history {
		if e.AmountINR > largest {
			largest = e.AmountINR
		}
	}
	if largest <= 0 {
		return list
	}
	for _, t := range deviationTiers {
		if amount/t.factor >= largest {
			return append(list, Contribution{Signal: SignalAmountDeviation, Points: t.points, Reason: t.sentence})
		}
	}
	return list
}
