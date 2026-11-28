package risk

import "github.com/RahulKumar1119/rukpay/backend/internal/store"

// Contribution is one line of an Explanation: the signal that added points, how
// many, and one calm sentence saying why. The type is defined in store, because
// risk imports store, so that the risk engine, the vault record and the JSON
// response share one shape.
type Contribution = store.Contribution

// Signal_Id values (Requirement 1.3). They name the source of a Contribution.
// The sentences that go with them live in sentences.go, which holds nothing
// else.
const (
	SignalAmount         = "amount"
	SignalBeneficiaryAge = "beneficiary_age"
	SignalLateNight      = "late_night"
	SignalUnknownCall    = "unknown_call"
	SignalRapidRetries   = "rapid_retries"

	SignalVelocity        = "velocity"
	SignalDailyFrequency  = "daily_frequency"
	SignalRepeatAttempts  = "repeat_attempts"
	SignalAmountDeviation = "amount_deviation"
	// SignalPatternPrefix starts the Signal_Id of a Fraud_Pattern Contribution.
	// The pattern id follows it.
	SignalPatternPrefix = "pattern_"

	SignalBehaviourCap    = "behaviour_cap"
	SignalBeneficiaryCare = "beneficiary_care"
	SignalScoreCap        = "score_cap"
)

// tier is one row of a tier table. A row holds its threshold, its points and
// its sentence together, so a Contribution can never carry points without the
// sentence of the same tier.
type tier struct {
	// min is the first amount or count that earns this row.
	min    int64
	points int
	// sentence is a constant from sentences.go.
	sentence string
}

// amountTiers lists the amount rows, highest first. The first row whose min
// the amount reaches wins. An amount below every row earns nothing. The table is
// read only: nothing writes to it after start-up.
var amountTiers = []tier{
	{min: 25000, points: 35, sentence: sentAmountTop},
	{min: 10000, points: 20, sentence: sentAmountHigh},
	{min: 2000, points: 10, sentence: sentAmountMid},
}

// pointsSentence maps the points that one of the shipped helpers returns to the
// sentence of that tier.
type pointsSentence struct {
	points   int
	sentence string
}

// ageSentences maps the points of agePoints to a sentence. Read only.
var ageSentences = []pointsSentence{
	{points: 25, sentence: sentAgeNew},
	{points: 10, sentence: sentAgeRecent},
}

// retrySentences maps the points of retryPoints to a sentence. Read only.
var retrySentences = []pointsSentence{
	{points: 10, sentence: sentRetriesHigh},
	{points: 5, sentence: sentRetriesMid},
}

// Caps and windows of the behavioural rules (Requirements 9, 10 and 11). The
// windows are in seconds.
const (
	// BehaviourCap is the most points that the behavioural Contributions can add
	// to a score together.
	BehaviourCap = 25
	// VelocityWindow, FrequencyWindow and RepeatWindow are the three windows
	// that the velocity, daily_frequency and repeat_attempts signals count
	// History_Events in.
	VelocityWindow  = 600
	FrequencyWindow = 86_400
	RepeatWindow    = 3_600
	// DeviationMinEvents is the fewest History_Events, and DeviationMinAmount the
	// smallest amount in rupees, for which amount_deviation can earn points.
	DeviationMinEvents = 5
	DeviationMinAmount = 2_000
)

// LargePaymentThreshold is the amount in rupees from which a first payment to a
// saved recipient counts as large. The beneficiary package reads it for the
// FIRST_PAYMENT_LARGE indicator (Requirement 19), so that the number lives in one
// place. Scoring itself does not use it.
const LargePaymentThreshold = 10_000

// HistoryEvent is the part of a stored History_Event that scoring reads. It is
// a small struct of its own, so that the engine takes no more from the store
// package than types and constants. Times are epoch seconds.
type HistoryEvent struct {
	// At is the time of the evaluation that the event records.
	At int64
	// AmountINR is the amount of that payment in rupees.
	AmountINR int64
	// BeneficiaryRef is the masked identifier of its recipient, or "" when the
	// payment named none.
	BeneficiaryRef string
}

// Input is everything Assess reads.
type Input struct {
	Signals Signals
	// Now is the evaluation time in epoch seconds. Assess reads no clock, so the
	// caller reads the time once and passes it here.
	Now int64
	// History is the History_Snapshot: the payer's earlier events, oldest first.
	// It holds no event of the payment being scored. Assess reads it and never
	// changes it, and its result does not depend on the order of events.
	History []HistoryEvent
	// BeneficiaryRef is the masked identifier of the recipient of the payment,
	// or "" when the payment names none.
	BeneficiaryRef string
	// Recipient is the saved recipient that the payment names, or nil when it
	// names none. Assess reads it and never changes it.
	Recipient *Recipient
}

// Assessment is the result of Assess.
type Assessment struct {
	// Score is the Risk_Score, 0 to 100. It equals the sum of the points of
	// Explanation, the cap lines included.
	Score int
	// State is the Initial_State that MapState gives Score.
	State store.VaultState
	// Baseline is the sum of the points that the five Signals earn.
	Baseline int
	// Behaviour is the sum of the behavioural points after the Behaviour_Cap,
	// 0 to BehaviourCap.
	Behaviour int
	// Care is the Care_Points of the recipient after the CareCap: 0, 5 or 10.
	Care int
	// Explanation lists the Contributions in the order of Requirement 1.3. It is
	// never nil on success, and it is empty when Score is 0.
	Explanation []Contribution
}

// Assess validates the signals and scores them, returning the Explanation with
// the score. The score is the baseline of the five Signals, plus the behavioural
// points that the History_Snapshot earns up to BehaviourCap, plus the care
// points of the recipient up to CareCap, up to MaxScore, and every point is a
// Contribution of the Explanation, the two cap lines included.
// It returns the *ValidationError of Validate for invalid signals and the zero
// Assessment with it.
//
// Assess is a pure function of its argument: it reads no clock, does no I/O,
// uses no randomness, keeps no state between calls and changes nothing it was
// given (Requirements 1.8, 9.5, 9.10).
func Assess(in Input) (Assessment, error) {
	if err := Validate(in.Signals); err != nil {
		return Assessment{}, err
	}
	s := in.Signals

	// The fixed order of Requirement 1.3 comes from this code path. A
	// Contribution is added only when its points are above 0.
	list := make([]Contribution, 0, 12)
	list = appendAmount(list, s.AmountINR)
	list = appendLooked(list, SignalBeneficiaryAge, agePoints(s.BeneficiaryAgeHrs), ageSentences)
	if s.IsLateNight {
		list = append(list, Contribution{Signal: SignalLateNight, Points: lateNightPoints, Reason: sentLateNight})
	}
	if s.ActiveUnknownCall {
		list = append(list, Contribution{Signal: SignalUnknownCall, Points: unknownCallPoints, Reason: sentUnknownCall})
	}
	list = appendLooked(list, SignalRapidRetries, retryPoints(s.RapidRetryCount), retrySentences)

	baseline := sumPoints(list)

	// Steps 6 to 10 of the pipeline: the behavioural Contributions, in their
	// fixed order. They read the History_Snapshot and nothing else, so with an
	// empty snapshot they add nothing and the score is the baseline.
	first := len(list)
	list = appendWindowSignals(list, in.Now, in.BeneficiaryRef, in.History)
	list = appendDeviation(list, s.AmountINR, in.History)
	list = appendPatterns(list, s.AmountINR, in.BeneficiaryRef, in.Now, in.History)
	behaviour := sumPoints(list[first:])

	// Step 11: the Behaviour_Cap applies to the behavioural sum once. A sum of
	// exactly BehaviourCap needs no line.
	if behaviour > BehaviourCap {
		list = append(list, Contribution{Signal: SignalBehaviourCap, Points: BehaviourCap - behaviour, Reason: sentBehaviourCap})
		behaviour = BehaviourCap
	}

	// Step 12: the Care_Points of the recipient, at most CareCap. They only add:
	// no recipient and a recipient on standard protection add nothing, so the
	// score is never below the baseline. The line comes after behaviour_cap and
	// before score_cap, so the order of Requirement 1.3 holds. The sentence is
	// chosen by the points alone.
	care := carePoints(in.Recipient)
	if care > CareCap {
		care = CareCap
	}
	list = appendLooked(list, SignalBeneficiaryCare, care, careSentences)

	// Step 13: the Risk_Score is at most MaxScore, judged on the baseline, the
	// capped behavioural points and the care points together. A total of exactly
	// MaxScore needs no line.
	total := baseline + behaviour + care
	if total > MaxScore {
		list = append(list, Contribution{Signal: SignalScoreCap, Points: MaxScore - total, Reason: sentScoreCap})
		total = MaxScore
	}

	return Assessment{
		Score:       total,
		State:       MapState(total),
		Baseline:    baseline,
		Behaviour:   behaviour,
		Care:        care,
		Explanation: list,
	}, nil
}

// sumPoints returns the sum of the points of the Contributions.
func sumPoints(list []Contribution) int {
	sum := 0
	for _, c := range list {
		sum += c.Points
	}
	return sum
}

// appendAmount adds the amount Contribution when the amount reaches a tier.
func appendAmount(list []Contribution, amountINR int64) []Contribution {
	for _, t := range amountTiers {
		if amountINR >= t.min {
			return append(list, Contribution{Signal: SignalAmount, Points: t.points, Reason: t.sentence})
		}
	}
	return list
}

// appendLooked adds a Contribution for points that one of the shipped helpers
// returned, with the sentence of the matching row. It adds nothing for 0 points.
func appendLooked(list []Contribution, signal string, points int, rows []pointsSentence) []Contribution {
	if points <= 0 {
		return list
	}
	for _, r := range rows {
		if r.points == points {
			return append(list, Contribution{Signal: signal, Points: points, Reason: r.sentence})
		}
	}
	return list
}
