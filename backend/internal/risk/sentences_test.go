package risk

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"pgregory.net/rapid"

	"github.com/RahulKumar1119/rukpay/backend/internal/redact"
)

// avoidWords is the Avoid_Words list of the risk-intelligence glossary: the
// Banned_Terms of the anti-panic steering document plus the extra words of this
// feature. A sentence holds one when the word occurs anywhere in it as a
// substring, compared without regard to letter case.
var avoidWords = []string{
	// Banned_Terms of the steering document.
	"FATAL ERROR", "SECURITY BREACH", "ILLEGAL TRANSACTION", "YOUR ACCOUNT WILL BE BLOCKED",
	"FRAUDSTER DETECTED", "heuristic mismatch", "anomaly score", "velocity threshold",
	"cryptographic hash", "HMAC", "SSL/TLS", "payload", "telemetry", "IP reputation", "rule engine",
	// Added by this feature.
	"velocity", "anomaly", "heuristic", "deviation", "suspicious", "fraud", "fraudulent",
	"criminal", "dangerous", "illegal", "scammer", "huge", "massive", "enormous", "urgent",
	"warning", "alert", "threat",
}

var (
	longLetterRun    = regexp.MustCompile(`\p{L}{13,}`)
	digitRun         = regexp.MustCompile(`[0-9]+`)
	digitBesideMark  = regexp.MustCompile(`[0-9][.,][0-9]`)
	maskedAccountFmt = regexp.MustCompile(`(?i)XXXX-XXXX-[0-9]{4}`)
	maskedPhoneFmt   = regexp.MustCompile(`(?i)\+91-XXXXXX[0-9]{4}`)
	amountWord       = regexp.MustCompile(`(?i)\b(rs|inr|rupees?|lakhs?|crores?|thousand|hundred|million)\b`)
)

// sentenceViolations returns every way that sentence breaks the wording rules
// of Requirement 3 criteria 2 to 6, one text per rule broken. An empty result
// means the sentence is calm. The check is a function of the text alone, so a
// later group can apply it to a Fraud_Pattern reason as well.
func sentenceViolations(sentence string) []string {
	var out []string
	add := func(format string, args ...any) { out = append(out, fmt.Sprintf(format, args...)) }

	// Criterion 2: one sentence of 1 to 20 words.
	if n := len(strings.Fields(sentence)); n < 1 || n > 20 {
		add("has %d words, want 1 to 20", n)
	}
	if first, _ := utf8.DecodeRuneInString(sentence); !unicode.IsUpper(first) {
		add("does not start with an uppercase letter")
	}
	if !strings.HasSuffix(sentence, ".") {
		add("does not end with a full stop")
	}
	if strings.Count(sentence, ".") != 1 {
		add("has %d full stops, want 1", strings.Count(sentence, "."))
	}
	if strings.ContainsAny(sentence, "!?…\n\r") {
		add("holds one of ! ? … or a line break")
	}
	if longLetterRun.MatchString(sentence) {
		add("holds a run of more than 12 letters")
	}

	// Criterion 3: no Avoid_Word as a substring.
	lower := strings.ToLower(sentence)
	for _, w := range avoidWords {
		if strings.Contains(lower, strings.ToLower(w)) {
			add("holds the avoid word %q", w)
		}
	}

	// Criterion 4: no long digit run, no @, no [REDACTED], no masked format.
	for _, run := range digitRun.FindAllString(sentence, -1) {
		if len(run) > 4 {
			add("holds a digit run of %d digits", len(run))
		}
	}
	if strings.Contains(sentence, "@") {
		add("holds @")
	}
	if strings.Contains(lower, strings.ToLower(redact.Replacement)) {
		add("holds %s", redact.Replacement)
	}
	if maskedAccountFmt.MatchString(sentence) || maskedPhoneFmt.MatchString(sentence) {
		add("holds text in a masked identifier format")
	}

	// Criterion 5: the amount in words only, with no rupee figure.
	if strings.Contains(sentence, "₹") {
		add("holds a rupee sign")
	}
	if m := amountWord.FindString(sentence); m != "" {
		add("holds the amount word %q", m)
	}
	for _, run := range digitRun.FindAllString(sentence, -1) {
		switch run {
		case "1000", "2000", "10000", "25000":
			add("holds the figure %s", run)
		}
	}
	if digitBesideMark.MatchString(sentence) {
		add("holds a digit beside a comma or full stop and another digit")
	}

	// Criterion 6: redact.Text leaves it unchanged.
	if redact.Text(sentence) != sentence {
		add("is changed by redact.Text")
	}
	return out
}

// sentenceFile reads sentences.go and returns the value of every string
// constant it declares, by name, and a text for everything in it that is not a
// string constant: an import, a variable, a type, a function, or a constant that
// is not a plain string literal. Reading the source means that a sentence added
// later is checked with no change to a test.
func sentenceFile(tb testing.TB) (consts map[string]string, problems []string) {
	tb.Helper()
	return scanSentenceSource(tb, nil)
}

// scanSentenceSource is sentenceFile over src. A nil src reads sentences.go from
// disk. A test passes source text to prove that the scan reports what it must.
func scanSentenceSource(tb testing.TB, src any) (consts map[string]string, problems []string) {
	tb.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "sentences.go", src, 0)
	if err != nil {
		tb.Fatalf("parse sentences.go: %v", err)
	}

	consts = make(map[string]string)
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			problems = append(problems, "declares a function")
			continue
		}
		if gen.Tok != token.CONST {
			problems = append(problems, "declares something other than a constant: "+gen.Tok.String())
			continue
		}
		for _, spec := range gen.Specs {
			vs := spec.(*ast.ValueSpec)
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					problems = append(problems, "constant "+name.Name+" has no string literal of its own")
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					problems = append(problems, "constant "+name.Name+" is not a string literal")
					continue
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					tb.Fatalf("unquote %s: %v", name.Name, err)
				}
				consts[name.Name] = value
			}
		}
	}
	return consts, problems
}

// Feature: risk-intelligence, Property 11: Every explanation sentence is calm
//
// The scan reads sentences.go and checks every constant it declares against
// Requirement 3 criteria 2 to 6. It fails when it finds no constant, so it
// cannot pass with nothing to check.
//
// **Validates: Requirements 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7**
func TestEveryExplanationSentenceIsCalm(t *testing.T) {
	consts, problems := sentenceFile(t)
	for _, p := range problems {
		t.Errorf("sentences.go %s, but it may declare string constants only (Requirement 3.7)", p)
	}
	if len(consts) == 0 {
		t.Fatal("found no string constant in sentences.go, so the check would pass with nothing to check")
	}

	for name, value := range consts {
		for _, v := range sentenceViolations(value) {
			t.Errorf("%s = %q %s", name, value, v)
		}
		// The design table is stricter than Requirement 3: the baseline
		// sentences hold no digit at all.
		if strings.ContainsAny(value, "0123456789") {
			t.Errorf("%s = %q holds a digit", name, value)
		}
	}
}

// Feature: risk-intelligence, Property 11: Every explanation sentence is calm
//
// The reason of a Contribution is fixed by its Signal_Id and tier alone: the
// same Signal_Id and tier give the same bytes for every input, and every reason
// that Assess returns is one of the constants of sentences.go, so no reason is
// built at run time (Requirement 3.7).
//
// **Validates: Requirements 3.1, 3.7**
func TestSameSignalAndTierGiveIdenticalReasonBytes(t *testing.T) {
	consts, _ := sentenceFile(t)
	known := make(map[string]bool, len(consts))
	for _, value := range consts {
		known[value] = true
	}

	// key names the Signal_Id and the tier. A cap line has one sentence whatever
	// its points, so its tier is not part of the key.
	key := func(c Contribution) string {
		switch refClass(c.Signal) {
		case refClassBehaviourCap, refClassScoreCap:
			return c.Signal
		}
		return c.Signal + "/" + strconv.Itoa(c.Points)
	}

	rapid.Check(t, func(t *rapid.T) {
		a, err := Assess(genInput().Draw(t, "first"))
		if err != nil {
			t.Fatalf("Assess returned an error for valid signals: %v", err)
		}
		b, err := Assess(genInput().Draw(t, "second"))
		if err != nil {
			t.Fatalf("Assess returned an error for valid signals: %v", err)
		}

		reasons := make(map[string]string)
		for _, c := range append(append([]Contribution{}, a.Explanation...), b.Explanation...) {
			// A Fraud_Pattern reason lives in the Pattern_Table, not in sentences.go.
			if !strings.HasPrefix(c.Signal, SignalPatternPrefix) && !known[c.Reason] {
				t.Fatalf("reason %q of %q is not a constant of sentences.go", c.Reason, c.Signal)
			}
			if seen, ok := reasons[key(c)]; ok && seen != c.Reason {
				t.Fatalf("%s has the reasons %q and %q", key(c), seen, c.Reason)
			}
			reasons[key(c)] = c.Reason
		}
	})
}

// TestSentenceCheckRejectsBadSentences guards the scan above: each text below
// breaks one rule and must be reported, and a calm text must not be.
func TestSentenceCheckRejectsBadSentences(t *testing.T) {
	bad := map[string]string{
		"no words":                "",
		"lower case start":        "this payment is large.",
		"no full stop":            "This payment is large",
		"two sentences":           "This is large. So is this.",
		"question mark":           "Is this payment large?",
		"exclamation mark":        "This payment is large!",
		"ellipsis":                "This payment is large… really.",
		"line break":              "This payment\nis large.",
		"21 words":                "One two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen sixteen seventeen eighteen nineteen twenty twentyone.",
		"13 letter word":          "This is an abcdefghijklm word.",
		"avoid word alert":        "This is an alert for you.",
		"avoid word substring":    "A fraudster is here.",
		"avoid word velocity":     "The velocity is high.",
		"avoid word deviation":    "This is a deviation.",
		"avoid word payload":      "The payload is large.",
		"digit run of five":       "It was tried 12345 times.",
		"at sign":                 "Write to me@example today.",
		"redacted text":           "The [REDACTED] value is hidden.",
		"masked account":          "Sent to XXXX-XXXX-4821 today.",
		"masked phone":            "Call +91-XXXXXX3210 today.",
		"rupee sign":              "It costs ₹ a lot.",
		"word Rs":                 "It costs Rs a lot.",
		"word INR":                "It costs inr a lot.",
		"word rupees":             "It costs rupees a lot.",
		"word lakh":               "It is a lakh.",
		"word crores":             "It is many crores.",
		"word thousand":           "It is a thousand.",
		"word hundred":            "It is a hundred.",
		"word million":            "It is a million.",
		"figure 2000":             "The figure is 2000 here.",
		"figure 1000":             "The figure is 1000 here.",
		"digit comma digit":       "The figure is 1,5 here.",
		"digit full stop digit":   "The figure is 1.5 here.",
		"redact changes the text": "Pin: 1234 is here.",
	}
	for name, sentence := range bad {
		if len(sentenceViolations(sentence)) == 0 {
			t.Errorf("%s: %q was not reported", name, sentence)
		}
	}

	good := []string{
		"Take a breath.",
		"This is a large payment.",
		"Your 15-minute pause starts now.",
	}
	for _, sentence := range good {
		if v := sentenceViolations(sentence); len(v) != 0 {
			t.Errorf("%q was reported: %v", sentence, v)
		}
	}
}

// TestSentenceFileScanReportsWhatIsNotAStringConstant guards the file scan. The
// real file is clean, so this runs the scan over source that is not: an import,
// a variable, a number constant and a function must each be reported, and the
// string constant must still be found.
func TestSentenceFileScanReportsWhatIsNotAStringConstant(t *testing.T) {
	const source = `package risk

import "strings"

var notConst = "x"

const (
	sentOK  = "Fine."
	sentNum = 5
)

func helper() {}
`
	consts, problems := scanSentenceSource(t, source)
	if len(consts) != 1 || consts["sentOK"] != "Fine." {
		t.Errorf("constants = %v, want only sentOK", consts)
	}
	want := []string{
		"declares something other than a constant: import",
		"declares something other than a constant: var",
		"constant sentNum is not a string literal",
		"declares a function",
	}
	if strings.Join(problems, "|") != strings.Join(want, "|") {
		t.Errorf("problems = %q, want %q", problems, want)
	}
}
