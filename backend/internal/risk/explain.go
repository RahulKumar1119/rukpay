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

// Input is everything Assess reads. Later groups add the evaluation time, the
// History_Snapshot and the recipient data.
type Input struct {
	Signals Signals
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
	// Explanation lists the Contributions in the order of Requirement 1.3. It is
	// never nil on success, and it is empty when Score is 0.
	Explanation []Contribution
}

// Assess validates the signals and scores them, returning the Explanation with
// the score. It returns the *ValidationError of Validate for invalid signals
// and the zero Assessment with it.
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
	list := make([]Contribution, 0, 5)
	list = appendAmount(list, s.AmountINR)
	list = appendLooked(list, SignalBeneficiaryAge, agePoints(s.BeneficiaryAgeHrs), ageSentences)
	if s.IsLateNight {
		list = append(list, Contribution{Signal: SignalLateNight, Points: lateNightPoints, Reason: sentLateNight})
	}
	if s.ActiveUnknownCall {
		list = append(list, Contribution{Signal: SignalUnknownCall, Points: unknownCallPoints, Reason: sentUnknownCall})
	}
	list = appendLooked(list, SignalRapidRetries, retryPoints(s.RapidRetryCount), retrySentences)

	// The five baseline rules add up to at most MaxScore, so no score_cap line
	// is needed yet. The behavioural and care points that later groups add can
	// push the total over it.
	baseline := 0
	for _, c := range list {
		baseline += c.Points
	}
	return Assessment{
		Score:       baseline,
		State:       MapState(baseline),
		Baseline:    baseline,
		Explanation: list,
	}, nil
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
