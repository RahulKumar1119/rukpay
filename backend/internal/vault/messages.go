package vault

import (
	"fmt"
	"strings"

	"github.com/RahulKumar1119/rukpay/backend/internal/redact"
	"github.com/RahulKumar1119/rukpay/backend/internal/risk"
	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// Every user-facing string in this package is a constant named msg... in this
// file. The tone follows the anti-panic rules: calm, plain, and framed as a
// protective pause, never as an accusation. No constant may contain a banned
// alarm phrase or a technical term (Requirements 11.4, 11.5). A test reads this
// file and checks every msg constant.
const (
	msgInstantPass   = "All clear. This payment looks like your usual kind of payment, so it can go ahead."
	msgCheckPassed   = "Thank you. Those answers look calm and clear, so this payment can go ahead."
	msgApproved      = "Thank you. Your family guardian has confirmed this payment, so it can go ahead."
	msgVetoed        = "This payment has been cancelled and your money stays where it is. If someone pressured you to pay, you can call the National Cybercrime Helpline on 1930."
	msgExpired       = "The 15-minute pause has finished. Please take a calm moment and confirm that you still want this payment to go ahead."
	msgReconfirmed   = "Thank you for confirming. This payment can go ahead."
	msgHelpline      = "National Cybercrime Helpline: 1930"
	msgReportOnline  = "Report online: cybercrime.gov.in"
	msgEvidenceTitle = "Payment stopped by RukPay"
)

// Sentence templates. Each has one %s for the reasons, written in plain English.
const (
	// msgRealityCheckFmt asks for the three safety questions.
	msgRealityCheckFmt = "Let's take a quick moment. Because of %s, please answer three short safety questions before this payment goes ahead."
	// msgLockedFmt is the protective-pause sentence for a payment locked on
	// arrival. It states the 15-minute hold and the reasons (Requirement 11.5).
	msgLockedFmt = "Take a breath. Because of %s, RukPay has placed a 15-minute safety pause on this payment so you have space to verify."
	// msgLockedFromCheckFmt is the same sentence for a payment locked by the
	// answers to the safety questions.
	msgLockedFromCheckFmt = "Take a breath. Because %s, RukPay has placed a 15-minute safety pause on this payment so you have space to verify."
)

// Reasons drawn from the signals. Only phrases for signals that are true are
// used.
const (
	msgReasonLargeAmount  = "a large amount"
	msgReasonNewRecipient = "a brand-new recipient"
	msgReasonFairlyNew    = "a fairly new recipient"
	msgReasonLateHour     = "the late hour"
	msgReasonUnknownCall  = "an active unknown call"
	msgReasonQuickRetries = "several quick retries"
	msgReasonFallback     = "a few unusual details"
)

// Reasons drawn from the safety-question answers.
const (
	msgAnswerOnCall     = "you are on a call with someone claiming to be an official"
	msgAnswerSecret     = "you were asked to keep this payment secret"
	msgAnswerNotChecked = "this payment has not been checked independently yet"
)

// Phrases drawn from the behavioural lines of the Explanation (Requirement 3.8).
// Each is a short clause of at most 8 words that continues the sentence "Because
// of ...", is chosen by the Signal_Id alone and never by the points, and holds no
// digit, name, amount or identifier. A test reads every constant whose name
// starts msgPhrase and checks it against Requirement 3.10, so a phrase added here
// is checked with no change to the test.
const (
	msgPhraseVelocity  = "several payments in a short time"
	msgPhraseDaily     = "many payments in one day"
	msgPhraseRepeat    = "repeated payments to the same recipient"
	msgPhraseDeviation = "an amount well above your recent payments"
	msgPhrasePattern   = "small payments followed by a large one"
)

// Thresholds for the signal phrases. They follow the first tier that earns
// points in the risk engine.
const (
	phraseLargeAmountINR = 10000
	phraseNewHours       = 24
	phraseFairlyNewHours = 72
	phraseRetryCount     = 2
)

// reasonFor returns the plain-language reason shown for a vault that starts in
// state. For REALITY_CHECK and LOCKED_15M it names the signals that are true and
// no others. The text is passed through redact.Text before it is stored
// (Requirement 11.3).
func reasonFor(state store.VaultState, s risk.Signals) string {
	var text string
	switch state {
	case store.StateInstantPass:
		text = msgInstantPass
	case store.StateRealityCheck:
		text = fmt.Sprintf(msgRealityCheckFmt, signalReasons(s))
	case store.StateLocked15M:
		text = fmt.Sprintf(msgLockedFmt, signalReasons(s))
	case store.StateGuardianApproved:
		text = msgApproved
	case store.StateVetoed:
		text = msgVetoed
	case store.StateExpired:
		text = msgExpired
	}
	return redact.Text(text)
}

// reasonForExplained returns the reason of a vault that starts in state, with the
// behavioural lines of its Explanation named after the signals (Requirement
// 3.8). For REALITY_CHECK and LOCKED_15M it takes the reasons that reasonFor
// names, then adds one phrase for each Contribution of ex that has more than 0
// points and a behavioural Signal_Id, in the order of ex, and joins them all in
// the form of joinPlain: one, "a and b", or "a, b and c". A Contribution of the
// baseline, of a cap, with 0 points, or with a Signal_Id that has no phrase adds
// nothing (Requirement 3.10).
//
// It returns exactly what reasonFor returns when ex has no such Contribution,
// and for every state that is not REALITY_CHECK or LOCKED_15M, whatever ex holds
// (Requirements 3.9, 3.11). So a payment that passes at once, and every reason
// written after creation, is as before.
//
// The fallback phrase is used only when the combined list is empty, and that
// case is the one reasonFor already covers: a list with a phrase in it is never
// empty.
func reasonForExplained(state store.VaultState, s risk.Signals, ex []risk.Contribution) string {
	if state != store.StateRealityCheck && state != store.StateLocked15M {
		return reasonFor(state, s)
	}
	phrases := behaviourPhrases(ex)
	if len(phrases) == 0 {
		return reasonFor(state, s)
	}

	joined := joinPlain(append(signalReasonList(s), phrases...))
	if state == store.StateRealityCheck {
		return redact.Text(fmt.Sprintf(msgRealityCheckFmt, joined))
	}
	return redact.Text(fmt.Sprintf(msgLockedFmt, joined))
}

// behaviourPhrases returns the phrase of each behavioural Contribution of ex that
// earned points, in the order of ex.
func behaviourPhrases(ex []risk.Contribution) []string {
	var phrases []string
	for _, c := range ex {
		if c.Points <= 0 {
			continue
		}
		if phrase, ok := phraseFor(c.Signal); ok {
			phrases = append(phrases, phrase)
		}
	}
	return phrases
}

// phraseFor returns the summary phrase of a Signal_Id, and false for a Signal_Id
// that has none. The choice rests on the Signal_Id alone. Every
// Fraud_Pattern Contribution shares one phrase, whichever pattern it is.
func phraseFor(signal string) (string, bool) {
	switch {
	case signal == risk.SignalVelocity:
		return msgPhraseVelocity, true
	case signal == risk.SignalDailyFrequency:
		return msgPhraseDaily, true
	case signal == risk.SignalRepeatAttempts:
		return msgPhraseRepeat, true
	case signal == risk.SignalAmountDeviation:
		return msgPhraseDeviation, true
	case strings.HasPrefix(signal, risk.SignalPatternPrefix):
		return msgPhrasePattern, true
	}
	return "", false
}

// reasonForAnswers returns the protective-pause sentence for a payment locked
// by the safety questions. It names the answers that caused the lock.
func reasonForAnswers(a Answers) string {
	var reasons []string
	if a.Coercion == AnswerYes {
		reasons = append(reasons, msgAnswerOnCall)
	}
	if a.Secrecy == AnswerYes {
		reasons = append(reasons, msgAnswerSecret)
	}
	if a.Verification == AnswerNo {
		reasons = append(reasons, msgAnswerNotChecked)
	}
	if len(reasons) == 0 {
		reasons = append(reasons, msgReasonFallback)
	}
	return redact.Text(fmt.Sprintf(msgLockedFromCheckFmt, joinPlain(reasons)))
}

// signalReasons names the signals of s that are true, in plain words, or the
// fallback phrase when none is.
func signalReasons(s risk.Signals) string {
	reasons := signalReasonList(s)
	if len(reasons) == 0 {
		return msgReasonFallback
	}
	return joinPlain(reasons)
}

// signalReasonList lists the reason phrases of the signals of s that are true,
// in the fixed order amount, recipient age, late hour, call, retries. It is
// empty when no signal is true.
func signalReasonList(s risk.Signals) []string {
	var reasons []string
	if s.AmountINR >= phraseLargeAmountINR {
		reasons = append(reasons, msgReasonLargeAmount)
	}
	switch {
	case s.BeneficiaryAgeHrs < phraseNewHours:
		reasons = append(reasons, msgReasonNewRecipient)
	case s.BeneficiaryAgeHrs < phraseFairlyNewHours:
		reasons = append(reasons, msgReasonFairlyNew)
	}
	if s.IsLateNight {
		reasons = append(reasons, msgReasonLateHour)
	}
	if s.ActiveUnknownCall {
		reasons = append(reasons, msgReasonUnknownCall)
	}
	if s.RapidRetryCount >= phraseRetryCount {
		reasons = append(reasons, msgReasonQuickRetries)
	}
	return reasons
}

// joinPlain joins items as "a", "a and b" or "a, b and c".
func joinPlain(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	default:
		return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
	}
}
