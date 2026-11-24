package vault

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/RahulKumar1119/rukpay/backend/internal/redact"
	"github.com/RahulKumar1119/rukpay/backend/internal/risk"
	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// avoidWords is the Avoid_Words list of the risk-intelligence glossary: the
// banned phrases and terms of the anti-panic steering document that
// pbt_messages_test.go already lists, plus the extra words of this feature. A
// text holds one when the word occurs anywhere in it as a substring, compared
// without regard to letter case.
func avoidWords() []string {
	extra := []string{
		"velocity", "anomaly", "heuristic", "deviation", "suspicious", "fraud", "fraudulent",
		"criminal", "dangerous", "illegal", "scammer", "huge", "massive", "enormous", "urgent",
		"warning", "alert", "threat",
	}
	return append(append(bannedPhrases(), bannedTerms()...), extra...)
}

var (
	phraseDigitRun        = regexp.MustCompile(`[0-9]+`)
	phraseDigitBesideMark = regexp.MustCompile(`[0-9][.,][0-9]`)
	phraseMaskedAccount   = regexp.MustCompile(`(?i)XXXX-XXXX-[0-9]{4}`)
	phraseMaskedPhone     = regexp.MustCompile(`(?i)\+91-XXXXXX[0-9]{4}`)
	phraseAmountWord      = regexp.MustCompile(`(?i)\b(rs|inr|rupees?|lakhs?|crores?|thousand|hundred|million)\b`)
)

// phraseViolations returns every way that phrase breaks Requirement 3.8 and 3.10:
// at most 8 words, the rules of Requirement 3 criteria 3 to 6 (no Avoid_Word, no
// long digit run, no @, no [REDACTED], no masked format, no rupee figure, text
// that redact.Text leaves alone), and the shape of a clause that continues a
// sentence: it starts in lower case and holds no full stop, comma or other
// mark that would break the joined list. An empty result means the phrase is
// acceptable.
func phraseViolations(phrase string) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }

	if n := len(strings.Fields(phrase)); n < 1 || n > 8 {
		add("has %d words, want 1 to 8", n)
	}
	if phrase != strings.TrimSpace(phrase) {
		add("has spaces at an end")
	}
	if phrase != "" && phrase != strings.ToLower(phrase[:1])+phrase[1:] {
		add("does not start in lower case")
	}
	if strings.ContainsAny(phrase, ".,!?…;:\n\r") {
		add("holds a mark that would break the joined sentence")
	}

	lower := strings.ToLower(phrase)
	for _, w := range avoidWords() {
		if strings.Contains(lower, strings.ToLower(w)) {
			add("holds the avoid word %q", w)
		}
	}
	for _, run := range phraseDigitRun.FindAllString(phrase, -1) {
		if len(run) > 4 {
			add("holds a digit run of %d digits", len(run))
		}
		switch run {
		case "1000", "2000", "10000", "25000":
			add("holds the figure %s", run)
		}
	}
	if strings.Contains(phrase, "@") {
		add("holds @")
	}
	if strings.Contains(lower, strings.ToLower(redact.Replacement)) {
		add("holds %s", redact.Replacement)
	}
	if phraseMaskedAccount.MatchString(phrase) || phraseMaskedPhone.MatchString(phrase) {
		add("holds text in a masked identifier format")
	}
	if strings.Contains(phrase, "₹") {
		add("holds a rupee sign")
	}
	if m := phraseAmountWord.FindString(phrase); m != "" {
		add("holds the amount word %q", m)
	}
	if phraseDigitBesideMark.MatchString(phrase) {
		add("holds a digit beside a comma or full stop and another digit")
	}
	if redact.Text(phrase) != phrase {
		add("is changed by redact.Text")
	}
	return out
}

// phraseConstants reads messages.go and returns the value of every constant whose
// name starts msgPhrase. Reading the source means that a phrase added later is
// checked with no change to a test (Requirement 3.10).
func phraseConstants(tb testing.TB) map[string]string {
	tb.Helper()
	found := make(map[string]string)
	for name, value := range messageConstants(tb) {
		if strings.HasPrefix(name, "msgPhrase") {
			found[name] = value
		}
	}
	return found
}

// Feature: risk-intelligence, Property 12: The vault reason keeps its old form and gains calm phrases
//
// Every summary phrase of messages.go meets Requirement 3.10 and Requirement 3.8:
// at most 8 words, and criteria 3 to 6 of Requirement 3. The test reads the
// constants whose names start msgPhrase from the source, so a phrase added later
// is checked with no change here, and it fails when it finds none.
//
// **Validates: Requirements 3.8, 3.10**
func TestEveryPhraseConstantMeetsTheWordingRules(t *testing.T) {
	phrases := phraseConstants(t)
	if len(phrases) == 0 {
		t.Fatal("found no msgPhrase constant in messages.go, so the check would pass with nothing to check")
	}

	byValue := make(map[string]string, len(phrases))
	for name, value := range phrases {
		for _, v := range phraseViolations(value) {
			t.Errorf("%s = %q %s", name, value, v)
		}
		if other, dup := byValue[value]; dup {
			t.Errorf("%s and %s have the same text %q, so the signals could not be told apart", name, other, value)
		}
		byValue[value] = name
	}
}

// TestPhraseCheckRejectsBadPhrases guards the check above: each text breaks one
// rule and must be reported, and a calm phrase must not be.
func TestPhraseCheckRejectsBadPhrases(t *testing.T) {
	bad := map[string]string{
		"nine words":          "one two three four five six seven eight nine",
		"empty":               "",
		"avoid word alert":    "an alert about payments",
		"avoid word velocity": "a velocity of payments",
		"avoid word fraud":    "a fraud pattern",
		"avoid substring":     "a fraudster nearby",
		"digit run of five":   "tried 12345 times",
		"at sign":             "mail me@example",
		"redacted":            "a [REDACTED] value",
		"masked account":      "to XXXX-XXXX-4821",
		"masked phone":        "to +91-XXXXXX3210",
		"rupee sign":          "costs ₹ a lot",
		"amount word":         "a thousand payments",
		"figure 2000":         "about 2000 here",
		"digit mark digit":    "about 1,5 here",
		"capital start":       "Several payments",
		"full stop":           "several payments.",
		"comma":               "several, quick payments",
		"changed by redact":   "pin 1234 payments",
	}
	for name, phrase := range bad {
		if len(phraseViolations(phrase)) == 0 {
			t.Errorf("%s: %q was not reported", name, phrase)
		}
	}
	for _, phrase := range []string{"several payments in a short time", "an amount well above your recent payments"} {
		if v := phraseViolations(phrase); len(v) != 0 {
			t.Errorf("%q was reported: %v", phrase, v)
		}
	}
}

// phraseOfSignal is the choice of phrase by Signal_Id, written out again from
// Requirement 3.8, with no code from messages.go but its constants.
func phraseOfSignal(signal string) (string, bool) {
	switch signal {
	case "velocity":
		return msgPhraseVelocity, true
	case "daily_frequency":
		return msgPhraseDaily, true
	case "repeat_attempts":
		return msgPhraseRepeat, true
	case "amount_deviation":
		return msgPhraseDeviation, true
	}
	if strings.HasPrefix(signal, "pattern_") {
		return msgPhrasePattern, true
	}
	return "", false
}

// Each behavioural Signal_Id has its own phrase, and no baseline signal, no cap
// line and no unknown id has one.
func TestPhraseIsChosenBySignalIdAlone(t *testing.T) {
	phrases := phraseConstants(t)
	inSource := make(map[string]bool, len(phrases))
	for _, value := range phrases {
		inSource[value] = true
	}

	withPhrase := []string{
		risk.SignalVelocity, risk.SignalDailyFrequency, risk.SignalRepeatAttempts,
		risk.SignalAmountDeviation, risk.SignalPatternPrefix + "test_then_large", risk.SignalPatternPrefix + "another_one",
	}
	for _, signal := range withPhrase {
		got, ok := phraseFor(signal)
		want, _ := phraseOfSignal(signal)
		if !ok || got != want || !inSource[got] {
			t.Errorf("phraseFor(%q) = %q, %v, want the constant %q", signal, got, ok, want)
		}
	}

	// The amount of a payment, its age, the late hour, the call, the retries and
	// the two caps are named by the sentence already, or by no phrase at all.
	withoutPhrase := []string{
		risk.SignalAmount, risk.SignalBeneficiaryAge, risk.SignalLateNight, risk.SignalUnknownCall,
		risk.SignalRapidRetries, risk.SignalBehaviourCap, risk.SignalScoreCap, "", "unknown", "velocity ", "Velocity",
	}
	for _, signal := range withoutPhrase {
		if got, ok := phraseFor(signal); ok {
			t.Errorf("phraseFor(%q) = %q, want no phrase", signal, got)
		}
	}
}

// reasonCase is one example of reasonForExplained.
type reasonCase struct {
	name        string
	state       store.VaultState
	signals     risk.Signals
	explanation []risk.Contribution
	want        string
}

func explLine(signal string, points int) risk.Contribution {
	return risk.Contribution{Signal: signal, Points: points, Reason: "A sentence."}
}

// Requirement 3.8: phrases follow the reasons the sentence already names, in
// Explanation order, joined as one, "a and b", or "a, b and c".
func TestReasonForExplainedExamples(t *testing.T) {
	locked := func(reasons string) string {
		return fmt.Sprintf(msgLockedFmt, reasons)
	}
	check := func(reasons string) string {
		return fmt.Sprintf(msgRealityCheckFmt, reasons)
	}
	lockedSignals := signalsLocked() // a large amount, a new recipient, the late hour, a call

	tests := []reasonCase{
		{
			name: "one phrase after four reasons", state: store.StateLocked15M, signals: lockedSignals,
			explanation: []risk.Contribution{explLine("amount", 35), explLine("velocity", 10)},
			want:        locked("a large amount, a brand-new recipient, the late hour, an active unknown call and several payments in a short time"),
		},
		{
			name: "two phrases in Explanation order", state: store.StateLocked15M, signals: lockedSignals,
			explanation: []risk.Contribution{explLine("velocity", 5), explLine("repeat_attempts", 5)},
			want:        locked("a large amount, a brand-new recipient, the late hour, an active unknown call, several payments in a short time and repeated payments to the same recipient"),
		},
		{
			name: "the same two phrases the other way round", state: store.StateLocked15M, signals: lockedSignals,
			explanation: []risk.Contribution{explLine("repeat_attempts", 5), explLine("velocity", 5)},
			want:        locked("a large amount, a brand-new recipient, the late hour, an active unknown call, repeated payments to the same recipient and several payments in a short time"),
		},
		{
			name: "a check with one reason and one phrase", state: store.StateRealityCheck, signals: signalsRealityCheck(),
			explanation: []risk.Contribution{explLine("amount_deviation", 15)},
			want:        check("a large amount, a brand-new recipient and an amount well above your recent payments"),
		},
		{
			name: "all five phrases", state: store.StateRealityCheck, signals: signalsRealityCheck(),
			explanation: []risk.Contribution{
				explLine("velocity", 5), explLine("daily_frequency", 5), explLine("repeat_attempts", 5),
				explLine("amount_deviation", 10), explLine("pattern_test_then_large", 15),
			},
			want: check("a large amount, a brand-new recipient, several payments in a short time, many payments in one day, " +
				"repeated payments to the same recipient, an amount well above your recent payments and small payments followed by a large one"),
		},
		{
			name: "two patterns give a phrase each", state: store.StateRealityCheck, signals: risk.Signals{AmountINR: 18000, BeneficiaryAgeHrs: 720},
			explanation: []risk.Contribution{explLine("pattern_test_then_large", 15), explLine("pattern_other", 5)},
			want:        check("a large amount, small payments followed by a large one and small payments followed by a large one"),
		},
		{
			name: "no reason of its own, so the phrase stands alone", state: store.StateRealityCheck, signals: risk.Signals{AmountINR: 450, BeneficiaryAgeHrs: 720},
			explanation: []risk.Contribution{explLine("velocity", 15)},
			want:        check("several payments in a short time"),
		},
		{
			name: "a cap line, a baseline line and a zero line add nothing", state: store.StateLocked15M, signals: lockedSignals,
			explanation: []risk.Contribution{explLine("amount", 35), explLine("velocity", 0), explLine("behaviour_cap", -5), explLine("score_cap", -10), explLine("unknown_signal", 5)},
			want:        reasonFor(store.StateLocked15M, lockedSignals),
		},
		{
			name: "a negative behavioural line adds nothing", state: store.StateLocked15M, signals: lockedSignals,
			explanation: []risk.Contribution{explLine("velocity", -5)},
			want:        reasonFor(store.StateLocked15M, lockedSignals),
		},
		{
			name: "an empty Explanation", state: store.StateRealityCheck, signals: signalsRealityCheck(),
			explanation: []risk.Contribution{}, want: reasonFor(store.StateRealityCheck, signalsRealityCheck()),
		},
		{
			name: "no Explanation", state: store.StateLocked15M, signals: lockedSignals,
			explanation: nil, want: reasonFor(store.StateLocked15M, lockedSignals),
		},
		{
			name: "a payment that passes at once keeps its sentence", state: store.StateInstantPass, signals: signalsInstantPass(),
			explanation: []risk.Contribution{explLine("velocity", 15), explLine("repeat_attempts", 10)},
			want:        msgInstantPass,
		},
		{
			name: "a settled state keeps its sentence", state: store.StateGuardianApproved, signals: lockedSignals,
			explanation: []risk.Contribution{explLine("velocity", 15)}, want: msgApproved,
		},
		{
			name: "a stopped payment keeps its sentence", state: store.StateVetoed, signals: lockedSignals,
			explanation: []risk.Contribution{explLine("velocity", 15)}, want: msgVetoed,
		},
		{
			name: "an expired pause keeps its sentence", state: store.StateExpired, signals: lockedSignals,
			explanation: []risk.Contribution{explLine("velocity", 15)}, want: msgExpired,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reasonForExplained(tt.state, tt.signals, tt.explanation)
			if got != redact.Text(tt.want) {
				t.Errorf("reasonForExplained =\n  %q\nwant\n  %q", got, tt.want)
			}
			checkCalm(t, "reason", got)
		})
	}
}

// Requirement 3.11: a reason written after creation, whatever the vault's
// Explanation holds, is the same fixed sentence as before. A vault created with
// behavioural lines keeps that Explanation, and none of its later reasons names a
// behavioural phrase.
func TestReasonsWrittenAfterCreationNameNoBehaviouralPhrase(t *testing.T) {
	phrases := phraseConstants(t)
	noPhrase := func(t *testing.T, what, reason string) {
		t.Helper()
		for name, phrase := range phrases {
			if strings.Contains(reason, phrase) {
				t.Errorf("%s: reason %q names the phrase %s", what, reason, name)
			}
		}
	}
	// A burst of recent payments gives every new vault a velocity line.
	burst := func(e *histEnv) {
		for i := 0; i < 8; i++ {
			e.seed(t, store.HistoryEvent{ID: seedID("b", i), At: histEpoch() - 30 - int64(i), AmountINR: 450, ExpiresAt: store.HistoryExpiry(histEpoch())})
		}
	}
	create := func(e *histEnv, sig risk.Signals) View {
		burst(e)
		res := e.evaluate(EvaluateRequest{Signals: sig})
		if res.err != nil {
			t.Fatalf("Evaluate = %v, want nil", res.err)
		}
		if !strings.Contains(res.view.Reason, msgPhraseVelocity) {
			t.Fatalf("the new vault's reason %q does not name the burst", res.view.Reason)
		}
		return res.view
	}

	t.Run("a reality check that locks", func(t *testing.T) {
		e := newHistEnv(false)
		v := create(e, signalsRealityCheck())
		answers := Answers{Coercion: AnswerYes, Secrecy: AnswerNo, Verification: AnswerYes}
		got, err := e.svc.RealityCheck(context.Background(), v.ID, answers)
		if err != nil {
			t.Fatalf("RealityCheck = %v", err)
		}
		if got.Reason != reasonForAnswers(answers) {
			t.Errorf("reason after the lock = %q, want %q", got.Reason, reasonForAnswers(answers))
		}
		noPhrase(t, "reality check lock", got.Reason)
	})
	t.Run("a calm reality check", func(t *testing.T) {
		e := newHistEnv(false)
		v := create(e, signalsRealityCheck())
		got, err := e.svc.RealityCheck(context.Background(), v.ID, Answers{Coercion: AnswerNo, Secrecy: AnswerNo, Verification: AnswerYes})
		if err != nil || got.Reason != redact.Text(msgCheckPassed) {
			t.Fatalf("RealityCheck = %q, %v, want %q", got.Reason, err, msgCheckPassed)
		}
	})
	t.Run("approve, veto, expiry and reconfirm", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			run  func(*histEnv, string) (View, error)
			want string
		}{
			{"approve", func(e *histEnv, id string) (View, error) { return e.svc.Approve(context.Background(), id) }, msgApproved},
			{"veto", func(e *histEnv, id string) (View, error) { return e.svc.Veto(context.Background(), id) }, msgVetoed},
			{"expiry", func(e *histEnv, id string) (View, error) {
				e.clock.AdvanceSeconds(LockDuration)
				return e.svc.Get(context.Background(), id)
			}, msgExpired},
			{"reconfirm", func(e *histEnv, id string) (View, error) {
				e.clock.AdvanceSeconds(LockDuration)
				return e.svc.Reconfirm(context.Background(), id)
			}, msgReconfirmed},
		} {
			e := newHistEnv(false)
			v := create(e, signalsLocked())
			got, err := tt.run(e, v.ID)
			if err != nil {
				t.Fatalf("%s = %v", tt.name, err)
			}
			if got.Reason != redact.Text(tt.want) {
				t.Errorf("%s: reason %q, want %q", tt.name, got.Reason, tt.want)
			}
			noPhrase(t, tt.name, got.Reason)
			// The Explanation is the one stored at creation, with its velocity line.
			if got.Explanation == nil || !hasSignal(*got.Explanation, risk.SignalVelocity) {
				t.Errorf("%s: the Explanation %v lost its velocity line", tt.name, got.Explanation)
			}
		}
	})
}

// hasSignal reports whether list holds a Contribution with the Signal_Id.
func hasSignal(list []risk.Contribution, signal string) bool {
	for _, c := range list {
		if c.Signal == signal {
			return true
		}
	}
	return false
}
