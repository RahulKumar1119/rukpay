package vault

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/redact"
	"github.com/RahulKumar1119/rukpay/backend/internal/risk"
	"github.com/RahulKumar1119/rukpay/backend/internal/store"
)

// bannedPhrases are the alarm phrases that user-facing text must never use.
func bannedPhrases() []string {
	return []string{
		"FATAL ERROR",
		"SECURITY BREACH",
		"ILLEGAL TRANSACTION",
		"YOUR ACCOUNT WILL BE BLOCKED",
		"FRAUDSTER DETECTED",
	}
}

// bannedTerms are the technical terms that user-facing text must never use.
func bannedTerms() []string {
	return []string{
		"heuristic mismatch",
		"anomaly score",
		"velocity threshold",
		"cryptographic hash",
		"HMAC",
		"SSL/TLS",
		"payload",
		"telemetry",
		"IP reputation",
		"rule engine",
	}
}

// checkCalm fails the test if text holds a banned phrase or term.
func checkCalm(tb rapid.TB, what, text string) {
	tb.Helper()
	lower := strings.ToLower(text)
	for _, banned := range append(bannedPhrases(), bannedTerms()...) {
		if strings.Contains(lower, strings.ToLower(banned)) {
			tb.Fatalf("%s %q contains the banned text %q", what, text, banned)
		}
	}
}

// messageConstants reads messages.go and returns the value of every string
// constant whose name starts with msg. Reading the source means that a constant
// added later is checked without a change to this test.
func messageConstants(tb testing.TB) map[string]string {
	tb.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "messages.go", nil, 0)
	if err != nil {
		tb.Fatalf("parse messages.go: %v", err)
	}

	found := make(map[string]string)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if !strings.HasPrefix(name.Name, "msg") || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					tb.Fatalf("unquote %s: %v", name.Name, err)
				}
				found[name.Name] = value
			}
		}
	}
	return found
}

// Feature: cooling-off-vault, Property 20: Messages are calm and redacted
//
// Every message constant is free of banned phrases and technical terms.
//
// **Validates: Requirements 11.4**
func TestEveryMessageConstantIsCalm(t *testing.T) {
	consts := messageConstants(t)
	if len(consts) < 20 {
		t.Fatalf("found only %d msg constants in messages.go, so the check would be vacuous", len(consts))
	}
	for name, value := range consts {
		lower := strings.ToLower(value)
		for _, banned := range append(bannedPhrases(), bannedTerms()...) {
			if strings.Contains(lower, strings.ToLower(banned)) {
				t.Errorf("%s = %q contains the banned text %q", name, value, banned)
			}
		}
		if redact.Text(value) != value {
			t.Errorf("%s = %q holds an OTP, PIN or password field", name, value)
		}
	}
}

// signalPhrases pairs each signal phrase with the condition that makes it true.
func signalPhrases(s risk.Signals) map[string]bool {
	return map[string]bool{
		msgReasonLargeAmount:  s.AmountINR >= 10000,
		msgReasonNewRecipient: s.BeneficiaryAgeHrs < 24,
		msgReasonFairlyNew:    s.BeneficiaryAgeHrs >= 24 && s.BeneficiaryAgeHrs < 72,
		msgReasonLateHour:     s.IsLateNight,
		msgReasonUnknownCall:  s.ActiveUnknownCall,
		msgReasonQuickRetries: s.RapidRetryCount >= 2,
	}
}

// Feature: cooling-off-vault, Property 20: Messages are calm and redacted
//
// For any state and any signals the reason is calm. A locked vault's reason
// states the 15-minute hold. A reason that names signals names exactly the true
// ones.
//
// **Validates: Requirements 11.4, 11.5**
func TestReasonsAreCalmAndNameOnlyTrueSignals(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		state := stateGen().Draw(rt, "state")
		s := signalsGen().Draw(rt, "signals")

		reason := reasonFor(state, s)
		if reason == "" {
			rt.Fatalf("reasonFor(%s, %+v) is empty", state, s)
		}
		checkCalm(rt, "reason", reason)
		if redact.Text(reason) != reason {
			rt.Fatalf("reason %q holds an OTP, PIN or password field", reason)
		}

		if state == store.StateLocked15M && !strings.Contains(reason, "15-minute") {
			rt.Fatalf("a locked reason %q does not state the 15-minute hold", reason)
		}

		if state != store.StateRealityCheck && state != store.StateLocked15M {
			return
		}
		anyTrue := false
		for phrase, isTrue := range signalPhrases(s) {
			anyTrue = anyTrue || isTrue
			if has := strings.Contains(reason, phrase); has != isTrue {
				rt.Fatalf("reason %q mentions %q = %v, but the signal is %v (signals %+v)", reason, phrase, has, isTrue, s)
			}
		}
		if !anyTrue && !strings.Contains(reason, msgReasonFallback) {
			rt.Fatalf("reason %q names no signal and not the fallback phrase", reason)
		}
	})
}

// Feature: cooling-off-vault, Property 20: Messages are calm and redacted
//
// The reason for a payment locked by the safety questions is calm, states the
// 15-minute hold, and names exactly the answers that caused the lock.
//
// **Validates: Requirements 11.4, 11.5**
func TestAnswerReasonsAreCalmAndNameTheCause(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		a := answersGen().Draw(rt, "answers")

		reason := reasonForAnswers(a)
		checkCalm(rt, "reason", reason)
		if !strings.Contains(reason, "15-minute") {
			rt.Fatalf("reason %q does not state the 15-minute hold", reason)
		}

		causes := map[string]bool{
			msgAnswerOnCall:     a.Coercion == AnswerYes,
			msgAnswerSecret:     a.Secrecy == AnswerYes,
			msgAnswerNotChecked: a.Verification == AnswerNo,
		}
		for phrase, isTrue := range causes {
			if has := strings.Contains(reason, phrase); has != isTrue {
				rt.Fatalf("reason %q mentions %q = %v, want %v for answers %+v", reason, phrase, has, isTrue, a)
			}
		}
	})
}

// sensitiveWordGen draws one of the field names that redact.Text removes, in a
// random mix of upper and lower case.
func sensitiveWordGen() *rapid.Generator[string] {
	return rapid.Custom(func(t *rapid.T) string {
		word := rapid.SampledFrom([]string{"otp", "pin", "password", "passcode"}).Draw(t, "word")
		var b strings.Builder
		for _, r := range word {
			if rapid.Bool().Draw(t, "upper") {
				b.WriteString(strings.ToUpper(string(r)))
			} else {
				b.WriteRune(r)
			}
		}
		return b.String()
	})
}

// Feature: cooling-off-vault, Property 20: Messages are calm and redacted
//
// For any text holding an OTP, PIN or password field with its value, redact.Text
// replaces the field and the value with [REDACTED] and leaves the rest alone.
//
// **Validates: Requirements 11.3**
func TestRedactReplacesSensitiveFieldsAndValues(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		prefix := rapid.OneOf(rapid.Just(""), rapid.StringMatching(`^[A-Za-z0-9 ,.]{0,30} $`)).Draw(rt, "prefix")
		suffix := rapid.OneOf(rapid.Just(""), rapid.StringMatching(`^ [A-Za-z0-9 ,.]{0,30}$`)).Draw(rt, "suffix")
		word := sensitiveWordGen().Draw(rt, "word")
		separator := rapid.SampledFrom([]string{":", "=", " ", ": ", "= ", " : "}).Draw(rt, "separator")
		value := rapid.StringMatching(`^[A-Za-z0-9]{3,12}$`).Draw(rt, "value")

		in := prefix + word + separator + value + suffix
		got := redact.Text(in)

		// The pattern ends at the first space after the value, so the prefix and
		// the suffix survive. Look for the value outside the replacement text,
		// which can itself spell a short value ("CTE" is inside "[REDACTED]").
		outside := strings.ReplaceAll(got, "[REDACTED]", "")
		if strings.Contains(outside, value) && !strings.Contains(prefix+suffix, value) {
			rt.Fatalf("Text(%q) = %q still holds the value %q", in, got, value)
		}
		if !strings.Contains(got, "[REDACTED]") {
			rt.Fatalf("Text(%q) = %q has no [REDACTED]", in, got)
		}
	})
}
