package risk

import (
	"fmt"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// This file holds the independent reference model for the Care_Points of
// Requirement 18 criteria 6 to 9 and for the whole score once care points join
// it (Requirement 1 criteria 3, 4 and 6, Requirement 9 criteria 1 and 2). It is
// written from the requirement text. It shares no code, no table and no
// constant with the risk package: it does not call carePoints, it spells the
// two protection levels as plain strings, and it writes out every combination of
// protection, verified time and Pressure_Flag as a row instead of as a rule. For
// the baseline and the behavioural lines it uses the models of reference_test.go
// and reference_behaviour_test.go, which are test code of their own.

// refCareRow is one row of the care table: a protection level, whether the
// recipient has a verified time, whether it has the Pressure_Flag, and the
// Care_Points that combination earns.
type refCareRow struct {
	protection string
	verified   bool
	pressure   bool
	points     int
}

// refCareRows lists every combination of Requirement 18 criteria 6 and 7.
// STANDARD protection earns 0 (criterion 6). EXTRA_CARE earns 10 with no
// verified time or with the Pressure_Flag, and 5 with a verified time and no
// flag (criterion 7).
var refCareRows = []refCareRow{
	{"STANDARD", false, false, 0},
	{"STANDARD", false, true, 0},
	{"STANDARD", true, false, 0},
	{"STANDARD", true, true, 0},
	{"EXTRA_CARE", false, false, 10},
	{"EXTRA_CARE", false, true, 10},
	{"EXTRA_CARE", true, true, 10},
	{"EXTRA_CARE", true, false, 5},
}

// refCarePoints returns the Care_Points of a recipient: 0 for a payment that
// names none, and the points of the row that matches it otherwise. It panics for
// a protection level the requirements do not define, so a generator that drew
// one would fail loudly and not be scored against a guess.
func refCarePoints(r *Recipient) int {
	if r == nil {
		return 0
	}
	for _, row := range refCareRows {
		if row.protection == string(r.Protection) && row.verified == r.Verified && row.pressure == r.Pressure {
			return row.points
		}
	}
	panic(fmt.Sprintf("the care table has no row for %+v", *r))
}

// refCareReasons holds the sentence of each care tier, written from the sentence
// table of the design and not read from sentences.go. The tier is the points.
var refCareReasons = map[int]string{
	5:  "This recipient is on your extra-care list.",
	10: "This recipient is new or has not been checked yet.",
}

// refCareReasonProblem returns a text for the first Contribution whose reason is
// not the sentence of its Signal_Id and tier, or "" when every one is. It adds
// the care sentences to refReasonProblem. The patterns of the real table are
// known to the model of reference_behaviour_test.go.
func refCareReasonProblem(explanation []Contribution) string {
	for _, c := range explanation {
		if c.Signal != "beneficiary_care" {
			if problem := refReasonProblem([]Contribution{c}, false); problem != "" {
				return problem
			}
			continue
		}
		want, ok := refCareReasons[c.Points]
		if !ok {
			return fmt.Sprintf("beneficiary_care has %d points, the model has a sentence for 5 and 10 only", c.Points)
		}
		if c.Reason != want {
			return fmt.Sprintf("beneficiary_care with %d points has the reason %q, want %q", c.Points, c.Reason, want)
		}
	}
	return ""
}

// refFull is what the model expects of a whole evaluation, care points included.
type refFull struct {
	// Entries are all expected Contributions in order, cap lines included.
	Entries []refEntry
	// Baseline is the sum of the five baseline lines.
	Baseline int
	// BehaviourSum is the behavioural sum before the cap of 25.
	BehaviourSum int
	// Behaviour is the behavioural sum after the cap.
	Behaviour int
	// Care is the care points, 0, 5 or 10.
	Care int
	// BeforeCap is baseline + behaviour + care, before the limit of 100.
	BeforeCap int
	// Score is the smaller of BeforeCap and 100.
	Score int
	// State is the Initial_State of Score, by the thresholds 40 and 75.
	State store.VaultState
}

// refAssessFull is the model of a whole evaluation. The order is the one of
// Requirement 1 criterion 3: baseline, behavioural, behaviour_cap,
// beneficiary_care, score_cap. The behaviour_cap line exists only for a
// behavioural sum above 25 (criterion 5), and the score_cap line only when the
// baseline, the capped behavioural points and the care points add up to more
// than 100 (criterion 6), so a total of exactly 100 has none.
func refAssessFull(table []refPattern, in Input) refFull {
	baseline := refBaseline(in.Signals)
	behaviour := refBehaviourEntries(table, in)

	r := refFull{
		Baseline:     refBaselineTotal(baseline),
		BehaviourSum: refBaselineTotal(behaviour),
		Care:         refCarePoints(in.Recipient),
	}
	r.Entries = append(append([]refEntry{}, baseline...), behaviour...)

	r.Behaviour = r.BehaviourSum
	if r.BehaviourSum > 25 {
		r.Entries = append(r.Entries, refEntry{Signal: "behaviour_cap", Points: 25 - r.BehaviourSum})
		r.Behaviour = 25
	}
	if r.Care > 0 {
		r.Entries = append(r.Entries, refEntry{Signal: "beneficiary_care", Points: r.Care})
	}

	r.BeforeCap = r.Baseline + r.Behaviour + r.Care
	r.Score = r.BeforeCap
	if r.BeforeCap > 100 {
		r.Entries = append(r.Entries, refEntry{Signal: "score_cap", Points: 100 - r.BeforeCap})
		r.Score = 100
	}
	r.State = frozenMapState(r.Score)
	return r
}
