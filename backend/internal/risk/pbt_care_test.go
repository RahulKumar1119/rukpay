package risk

import (
	"reflect"
	"sort"
	"testing"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// This file tests Property 9, and re-runs the guarantees of Properties 1, 3, 4
// and 5 with inputs that can earn care points. The shared generator genInput
// draws recipients that earn none, see genRecipientWithoutCare, so the inputs
// that do come from genCareInput below.

// genCareInput draws a valid Input with any recipient: none, STANDARD, and each
// EXTRA_CARE case. The Signals and the History_Snapshot come from the shared
// generators, and the Signals are steered towards the places where care points
// matter:
//
//   - a baseline of 80 to 100, where the care points reach the limit of 100 and
//     the score_cap line;
//   - a baseline of 75 or more, the lock guarantee;
//   - a baseline of 0, the pass guarantee, where the most that can be added is
//     the behavioural 25 and the care 10;
//   - baselines of 30 to 70 that sit within 10 points of the thresholds 40 and
//     75, so that the care points move the state.
func genCareInput() *rapid.Generator[Input] {
	nearThresholds := []Signals{
		{AmountINR: 25000, BeneficiaryAgeHrs: 720},                                             // 35
		{AmountINR: 10000, BeneficiaryAgeHrs: 720, IsLateNight: true},                          // 30
		{AmountINR: 25000, BeneficiaryAgeHrs: 30, ActiveUnknownCall: true, RapidRetryCount: 2}, // 70
		courierCustomsSignals, // 65
		kycExpirySignals,      // 40
		mediumPresetSignals,   // 45
		safePresetSignals,     // 0
	}
	return rapid.Custom(func(t *rapid.T) Input {
		var in Input
		// rapid favours small numbers, so the kinds that matter most come first.
		switch rapid.IntRange(0, 11).Draw(t, "care_input_kind") {
		case 0, 1:
			in = genSaturatedInput().Draw(t, "saturated_input")
		case 2, 3:
			in = genInputFor(rapid.SampledFrom(nearThresholds).Draw(t, "threshold_signals")).Draw(t, "threshold_input")
		case 4, 5:
			in = genInputFor(genHeavySignals().Draw(t, "heavy_signals")).Draw(t, "heavy_input")
		case 6:
			in = genInputFor(genLockedSignals().Draw(t, "locked_signals")).Draw(t, "locked_input")
		case 7, 8:
			in = genInputFor(genZeroBaselineSignals().Draw(t, "zero_signals")).Draw(t, "zero_input")
		default:
			in = genInput().Draw(t, "any_input")
		}
		in.Recipient = genRecipient().Draw(t, "recipient")
		return in
	})
}

// genSaturatedInput draws an Input whose Signals earn no baseline point and whose
// History_Snapshot holds 8 to 40 events to the recipient of the payment, all
// inside the velocity window. With 8 to 9 such events the behavioural sum is
// exactly 25, and with 10 or more it is above 25 and the behaviour_cap line
// appears. With any care points on top, the total reaches its largest value for
// a baseline of 0: 25 plus 10, which is 35.
func genSaturatedInput() *rapid.Generator[Input] {
	return rapid.Custom(func(t *rapid.T) Input {
		now := genNow().Draw(t, "now")
		ref := rapid.SampledFrom([]string{"r***@okaxis", "XXXX-XXXX-4821", "+91-XXXXXX3210"}).Draw(t, "ref")
		events := make([]HistoryEvent, rapid.IntRange(8, 40).Draw(t, "event_count"))
		for i := range events {
			events[i] = HistoryEvent{
				At:             timeBefore(now, rapid.Int64Range(0, 599).Draw(t, "age")),
				AmountINR:      genSmallAmount().Draw(t, "amount"),
				BeneficiaryRef: ref,
			}
		}
		sort.SliceStable(events, func(i, j int) bool { return events[i].At < events[j].At })
		return Input{
			Signals:        genZeroBaselineSignals().Draw(t, "signals"),
			Now:            now,
			History:        events,
			BeneficiaryRef: ref,
		}
	})
}

// careLines returns the beneficiary_care Contributions of an Explanation.
func careLines(explanation []Contribution) []Contribution {
	var out []Contribution
	for _, c := range explanation {
		if c.Signal == SignalBeneficiaryCare {
			out = append(out, c)
		}
	}
	return out
}

// without returns the Contributions of an Explanation except those with one of
// the given Signal_Ids, in their order. The result is never nil.
func without(explanation []Contribution, signals ...string) []Contribution {
	out := []Contribution{}
next:
	for _, c := range explanation {
		for _, s := range signals {
			if c.Signal == s {
				continue next
			}
		}
		out = append(out, c)
	}
	return out
}

// assessOK runs Assess and stops the test when it returns an error for a valid
// input.
func assessOK(t *rapid.T, in Input) Assessment {
	t.Helper()
	a, err := Assess(in)
	if err != nil {
		t.Fatalf("Assess(%+v) returned an error for valid signals: %v", in, err)
	}
	return a
}

// Feature: risk-intelligence, Property 9: Care points only add and follow the recipient
//
// The care Contribution follows the recipient and nothing else. A payment with no
// recipient or a STANDARD recipient has none and a Care of 0. An EXTRA_CARE
// recipient with no verified time, or with the Pressure_Flag, has one of 10
// points. An EXTRA_CARE recipient with a verified time and no flag has one of 5
// points. The reason is the sentence of the tier, chosen by the points alone, at
// most one care line exists, and no care line is above the cap of 10. The test
// fails when any of the three cases (0, 5, 10) never came up.
//
// **Validates: Requirements 18.6, 18.7, 18.8, 9.2**
func TestCarePointsFollowTheRecipient(t *testing.T) {
	const careLimit = 10 // Requirement 9 criterion 2
	seen := map[int]int{}

	// Every care reason is a string constant of sentences.go, so the sentence
	// scan covers it. A sentence written into another file would escape the scan.
	consts, _ := sentenceFile(t)
	known := make(map[string]bool, len(consts))
	for _, value := range consts {
		known[value] = true
	}

	rapid.Check(t, func(t *rapid.T) {
		in := genCareInput().Draw(t, "input")
		a := assessOK(t, in)

		want := refCarePoints(in.Recipient)
		if a.Care != want {
			t.Fatalf("Care = %d for the recipient %+v, want %d", a.Care, in.Recipient, want)
		}
		if a.Care < 0 || a.Care > careLimit {
			t.Fatalf("Care = %d, want 0 to %d", a.Care, careLimit)
		}

		lines := careLines(a.Explanation)
		switch {
		case want == 0 && len(lines) != 0:
			t.Fatalf("a recipient that earns 0 care points has the line %+v", lines)
		case want > 0 && len(lines) != 1:
			t.Fatalf("a recipient that earns %d care points has %d care lines: %+v", want, len(lines), lines)
		case want > 0:
			c := lines[0]
			if c.Points != want || c.Points > careLimit {
				t.Fatalf("care line has %d points, want %d and at most %d", c.Points, want, careLimit)
			}
			if c.Reason != refCareReasons[want] {
				t.Fatalf("care line with %d points has the reason %q, want %q", want, c.Reason, refCareReasons[want])
			}
			if !known[c.Reason] {
				t.Fatalf("care reason %q is not a constant of sentences.go", c.Reason)
			}
		}
		seen[want]++
	})

	for _, points := range []int{0, 5, 10} {
		if seen[points] == 0 {
			t.Fatalf("no generated input earned %d care points, so the check passed with nothing to check (seen %v)", points, seen)
		}
	}
}

// Feature: risk-intelligence, Property 9: Care points only add and follow the recipient
//
// The care line sits after the baseline and behavioural lines and after the
// behaviour_cap line, and before the score_cap line, which is the last line of
// the Explanation when it exists. The test fails when it never saw the care line
// next to each of the two cap lines.
//
// **Validates: Requirements 18.8, 1.3, 1.6**
func TestCareLineSitsBetweenTheBehaviourCapAndTheScoreCap(t *testing.T) {
	var afterBehaviourCap, beforeScoreCap int

	rapid.Check(t, func(t *rapid.T) {
		in := genCareInput().Draw(t, "input")
		a := assessOK(t, in)

		var care, behaviourCapAt, scoreCapAt = -1, -1, -1
		for i, c := range a.Explanation {
			switch refClass(c.Signal) {
			case refClassCare:
				care = i
			case refClassBehaviourCap:
				behaviourCapAt = i
			case refClassScoreCap:
				scoreCapAt = i
			}
		}

		if scoreCapAt >= 0 && scoreCapAt != len(a.Explanation)-1 {
			t.Fatalf("score_cap is at %d of %d lines, want the last line: %+v", scoreCapAt, len(a.Explanation), a.Explanation)
		}
		if care < 0 {
			return
		}
		for i, c := range a.Explanation {
			switch class := refClass(c.Signal); {
			case i < care && (class == refClassCare || class == refClassScoreCap):
				t.Fatalf("%q at %d comes before the care line at %d: %+v", c.Signal, i, care, a.Explanation)
			case i > care && class != refClassScoreCap:
				t.Fatalf("%q at %d comes after the care line at %d, only score_cap may: %+v", c.Signal, i, care, a.Explanation)
			}
		}
		if behaviourCapAt >= 0 {
			if behaviourCapAt > care {
				t.Fatalf("behaviour_cap at %d comes after the care line at %d: %+v", behaviourCapAt, care, a.Explanation)
			}
			afterBehaviourCap++
		}
		if scoreCapAt >= 0 {
			if scoreCapAt < care {
				t.Fatalf("score_cap at %d comes before the care line at %d: %+v", scoreCapAt, care, a.Explanation)
			}
			beforeScoreCap++
		}
	})

	if afterBehaviourCap == 0 || beforeScoreCap == 0 {
		t.Fatalf("the care line was seen after behaviour_cap %d times and before score_cap %d times, want both above 0",
			afterBehaviourCap, beforeScoreCap)
	}
}

// Feature: risk-intelligence, Property 9: Care points only add and follow the recipient
//
// The whole Explanation, the score and the state equal the reference model, which
// writes the care rule out as a table. The score_cap line is judged on the
// baseline, the capped behavioural points and the care points together: it
// exists exactly when that total is above 100 and takes it to 100, and a total
// of exactly 100 has no such line. The test fails when it never saw a total over
// 100 that only the care points caused, a total of exactly 100 with care points,
// or all three of behaviour_cap, beneficiary_care and score_cap in one
// Explanation.
//
// **Validates: Requirements 18.6, 18.7, 18.8, 1.6, 1.12, 9.1, 9.2, 9.12**
func TestAssessWithCareMatchesTheReferenceModel(t *testing.T) {
	requireModelPatternTable(t)
	var capByCareAlone, exactly100, allThree, capWithCare int

	rapid.Check(t, func(t *rapid.T) {
		in := genCareInput().Draw(t, "input")
		a := assessOK(t, in)
		want := refAssessFull(refPatternTable, in)

		var got []refEntry
		var sum int
		for _, c := range a.Explanation {
			got = append(got, refEntry{Signal: c.Signal, Points: c.Points})
			sum += c.Points
		}
		if !equalEntries(got, want.Entries) {
			t.Fatalf("Explanation = %v, reference model = %v (baseline %d, behaviour sum %d, care %d)",
				got, want.Entries, want.Baseline, want.BehaviourSum, want.Care)
		}
		if a.Baseline != want.Baseline || a.Behaviour != want.Behaviour || a.Care != want.Care ||
			a.Score != want.Score || a.State != want.State {
			t.Fatalf("Baseline, Behaviour, Care, Score, State = %d, %d, %d, %d, %q, reference model = %d, %d, %d, %d, %q",
				a.Baseline, a.Behaviour, a.Care, a.Score, a.State,
				want.Baseline, want.Behaviour, want.Care, want.Score, want.State)
		}
		if sum != a.Score {
			t.Fatalf("the points add up to %d, Score = %d: %+v", sum, a.Score, a.Explanation)
		}
		if problem := refCareReasonProblem(a.Explanation); problem != "" {
			t.Fatal(problem)
		}

		// The cap numbers written out. The limit is judged with the care points.
		_, hasScoreCap := lineOf(a.Explanation, SignalScoreCap)
		if hasScoreCap != (want.BeforeCap > 100) {
			t.Fatalf("score_cap line present = %v with baseline + behaviour + care = %d", hasScoreCap, want.BeforeCap)
		}
		if a.Score > 100 {
			t.Fatalf("Score = %d, want at most 100", a.Score)
		}

		if want.Care > 0 {
			if want.BeforeCap > 100 {
				capWithCare++
				if want.Baseline+want.Behaviour <= 100 {
					capByCareAlone++
				}
			}
			if want.BeforeCap == 100 {
				exactly100++
			}
			if want.BehaviourSum > 25 && want.BeforeCap > 100 {
				allThree++
			}
		}
	})

	if capByCareAlone == 0 || exactly100 == 0 || allThree == 0 {
		t.Fatalf("a total over 100 caused by care alone was seen %d times, a total of exactly 100 with care %d times, "+
			"all three of behaviour_cap, beneficiary_care and score_cap %d times (%d capped with care): want each above 0",
			capByCareAlone, exactly100, allThree, capWithCare)
	}
}

// lineOf returns the Contribution with the Signal_Id and true, or the zero
// Contribution and false.
func lineOf(explanation []Contribution, signal string) (Contribution, bool) {
	for _, c := range explanation {
		if c.Signal == signal {
			return c, true
		}
	}
	return Contribution{}, false
}

// Feature: risk-intelligence, Property 9: Care points only add and follow the recipient
//
// With a nil recipient Assess returns what it returned before care points
// existed: the whole Explanation, the score and the state of the model that has
// no care rule, and a Care of 0. A STANDARD recipient, with any verified time and
// flag, gives exactly the result of no recipient. Any recipient only adds: the
// score is never below that of no recipient, it is above it by at most the care
// points, the baseline and behavioural lines are the same, and the score is never
// below the baseline. Changing an EXTRA_CARE recipient to STANDARD lowers the
// score by at most 10 and never below the baseline. The test fails when it never
// saw a drop of 10, a drop of 5, or a drop smaller than the care points because
// of the cap of 100.
//
// **Validates: Requirements 18.6, 18.9, 9.3, 9.4, 23.1**
func TestCareOnlyAddsAndNoRecipientGivesThePreviousResult(t *testing.T) {
	requireModelPatternTable(t)
	var drop10, drop5, dropCut int

	rapid.Check(t, func(t *rapid.T) {
		in := genCareInput().Draw(t, "input")
		a := assessOK(t, in)

		none := cloneInput(in)
		none.Recipient = nil
		base := assessOK(t, none)

		// The previous result: the model of an evaluation that names no recipient.
		prev := refAssess(refPatternTable, none)
		var got []refEntry
		for _, c := range base.Explanation {
			got = append(got, refEntry{Signal: c.Signal, Points: c.Points})
		}
		if !equalEntries(got, prev.Entries) {
			t.Fatalf("with no recipient Explanation = %v, previous result = %v", got, prev.Entries)
		}
		if base.Score != prev.Score || base.State != prev.State || base.Behaviour != prev.Behaviour || base.Care != 0 {
			t.Fatalf("with no recipient Score, State, Behaviour, Care = %d, %q, %d, %d, previous result = %d, %q, %d, 0",
				base.Score, base.State, base.Behaviour, base.Care, prev.Score, prev.State, prev.Behaviour)
		}
		if problem := refReasonProblem(base.Explanation, false); problem != "" {
			t.Fatal(problem)
		}

		// STANDARD gives the result of no recipient, whatever else the record says.
		std := cloneInput(in)
		std.Recipient = &Recipient{
			Protection: store.ProtectionStandard,
			Verified:   rapid.Bool().Draw(t, "standard_verified"),
			Pressure:   rapid.Bool().Draw(t, "standard_pressure"),
		}
		if s := assessOK(t, std); !reflect.DeepEqual(s, base) {
			t.Fatalf("a STANDARD recipient gives %+v, no recipient gives %+v", s, base)
		}

		// Care only adds.
		if a.Score < base.Score || a.Score-base.Score > a.Care {
			t.Fatalf("Score = %d with %d care points, %d with no recipient", a.Score, a.Care, base.Score)
		}
		if a.Score < a.Baseline {
			t.Fatalf("Score = %d is below Baseline = %d", a.Score, a.Baseline)
		}
		if a.Baseline != base.Baseline || a.Behaviour != base.Behaviour {
			t.Fatalf("the recipient changed Baseline %d to %d or Behaviour %d to %d", base.Baseline, a.Baseline, base.Behaviour, a.Behaviour)
		}
		if refStateRank(a.State) < refStateRank(base.State) {
			t.Fatalf("State = %q is lower than %q with no recipient", a.State, base.State)
		}
		// Nothing but the care line and the score cap line differs.
		if got, want := without(a.Explanation, SignalBeneficiaryCare, SignalScoreCap),
			without(base.Explanation, SignalScoreCap); !reflect.DeepEqual(got, want) {
			t.Fatalf("the recipient changed lines other than care and score_cap: %+v, with no recipient %+v", got, want)
		}

		// EXTRA_CARE to STANDARD.
		if in.Recipient == nil || in.Recipient.Protection != store.ProtectionExtraCare {
			return
		}
		lowered := cloneInput(in)
		lowered.Recipient.Protection = store.ProtectionStandard
		l := assessOK(t, lowered)
		drop := a.Score - l.Score
		if drop < 0 || drop > 10 {
			t.Fatalf("EXTRA_CARE to STANDARD changed the score from %d to %d, want a drop of 0 to 10", a.Score, l.Score)
		}
		if l.Score < a.Baseline {
			t.Fatalf("EXTRA_CARE to STANDARD gives Score = %d, below the baseline %d", l.Score, a.Baseline)
		}
		switch {
		case drop == 10:
			drop10++
		case drop == 5:
			drop5++
		case drop < a.Care:
			dropCut++
		}
	})

	if drop10 == 0 || drop5 == 0 || dropCut == 0 {
		t.Fatalf("EXTRA_CARE to STANDARD dropped the score by 10 in %d runs, by 5 in %d runs and by less than the care points "+
			"because of the cap of 100 in %d runs: want each above 0", drop10, drop5, dropCut)
	}
}

// Feature: risk-intelligence, Property 5: Lock, pass and preset guarantees
//
// The guarantees hold with care points. A baseline of 75 or more gives a score of
// at least 75 and LOCKED_15M, whatever the recipient. A baseline of 0 gives a
// score of at most 35 (the behavioural 25 and the care 10) and INSTANT_PASS,
// whatever the history and the recipient. A baseline below 40 with 0 behavioural
// points and 0 care points gives a score below 40 and INSTANT_PASS. The state is
// never lower than the state of the baseline. The test fails when one of the
// three guarded cases never came up.
//
// **Validates: Requirements 9.4, 9.6, 9.7, 9.8, 9.12**
func TestCareKeepsTheLockAndPassGuarantees(t *testing.T) {
	var locked, zero, lowBaseline, reached35, withCare int

	rapid.Check(t, func(t *rapid.T) {
		in := genCareInput().Draw(t, "input")
		a := assessOK(t, in)

		if refStateRank(a.State) < refStateRank(frozenMapState(a.Baseline)) {
			t.Fatalf("State = %q is lower than the baseline state %q", a.State, frozenMapState(a.Baseline))
		}
		if a.Care > 0 {
			withCare++
		}
		if a.Baseline >= 75 {
			locked++
			if a.Score < 75 || a.State != store.StateLocked15M {
				t.Fatalf("baseline %d with recipient %+v gives score %d and state %q, want at least 75 and %q",
					a.Baseline, in.Recipient, a.Score, a.State, store.StateLocked15M)
			}
		}
		if a.Baseline == 0 {
			zero++
			if a.Score > 35 || a.State != store.StateInstantPass {
				t.Fatalf("baseline 0 with %d events and recipient %+v gives score %d and state %q, want at most 35 and %q",
					len(in.History), in.Recipient, a.Score, a.State, store.StateInstantPass)
			}
			if a.Score == 35 {
				reached35++
			}
		}
		if a.Baseline < 40 && a.Behaviour == 0 && a.Care == 0 {
			lowBaseline++
			if a.Score >= 40 || a.State != store.StateInstantPass {
				t.Fatalf("baseline %d with no extra points gives score %d and state %q, want below 40 and %q",
					a.Baseline, a.Score, a.State, store.StateInstantPass)
			}
		}
	})

	if locked == 0 || zero == 0 || lowBaseline == 0 || withCare == 0 {
		t.Fatalf("guarded cases seen: baseline of 75 or more %d, baseline of 0 %d, low baseline with no extra points %d, "+
			"inputs with care points %d (score of exactly 35 with baseline 0: %d): want each above 0", locked, zero, lowBaseline, withCare, reached35)
	}
}

// Feature: risk-intelligence, Property 9: Care points only add and follow the recipient
//
// Assess leaves the Recipient it is given as it was: the pointer and every field
// are unchanged after a call, and two calls on the same input give equal
// Assessments. A different recipient pointer with equal fields gives an equal
// Assessment, so no result depends on the identity of the pointer.
//
// **Validates: Requirements 9.5, 9.10, 18.9**
func TestAssessLeavesTheRecipientUnchanged(t *testing.T) {
	var checked int

	rapid.Check(t, func(t *rapid.T) {
		in := genCareInput().Draw(t, "input")
		before := cloneInput(in)
		pointer := in.Recipient

		first := assessOK(t, in)
		second := assessOK(t, in)

		if in.Recipient != pointer {
			t.Fatalf("Assess replaced the Recipient pointer")
		}
		if !reflect.DeepEqual(in, before) {
			t.Fatalf("Assess changed its input: now %+v and recipient %+v, was %+v and recipient %+v",
				in, in.Recipient, before, before.Recipient)
		}
		if !reflect.DeepEqual(first, second) {
			t.Fatalf("two calls differ: %+v and %+v", first, second)
		}
		if in.Recipient != nil {
			checked++
			// The same fields in another struct give the same result.
			copied := cloneInput(in)
			if c := assessOK(t, copied); !reflect.DeepEqual(c, first) {
				t.Fatalf("an equal copy of the recipient gives %+v, want %+v", c, first)
			}
		}
	})

	if checked == 0 {
		t.Fatal("no generated input had a recipient, so the check passed with nothing to check")
	}
}
