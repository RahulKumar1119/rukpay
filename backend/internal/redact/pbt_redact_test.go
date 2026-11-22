package redact

import (
	"flag"
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

// minChecks is the minimum number of generated inputs per property run. rapid
// defaults to 100, so the flag is raised here instead of relying on
// -rapid.checks on the command line. A larger explicit value still wins.
const minChecks = 1000

func init() {
	f := flag.Lookup("rapid.checks")
	if f == nil {
		return
	}
	n, err := strconv.Atoi(f.Value.String())
	if err == nil && n >= minChecks {
		return
	}
	if err := flag.Set("rapid.checks", strconv.Itoa(minChecks)); err != nil {
		panic("redact: raise rapid.checks: " + err.Error())
	}
}

// fieldGen draws one of the field names that Text removes, with each letter in a
// random case.
func fieldGen() *rapid.Generator[string] {
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

// For any field name, separator and value, with digits around them that cannot
// form a field name, Text replaces the field and its value with [REDACTED] and
// keeps everything else exactly as it was.
func TestTextReplacesExactlyTheFieldAndItsValue(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		prefix := rapid.OneOf(rapid.Just(""), rapid.StringMatching(`^[0-9]{0,10} $`)).Draw(rt, "prefix")
		suffix := rapid.OneOf(rapid.Just(""), rapid.StringMatching(`^ [0-9]{0,10}$`)).Draw(rt, "suffix")
		field := fieldGen().Draw(rt, "field")
		separator := rapid.SampledFrom([]string{":", "=", " ", ": ", "= ", " : "}).Draw(rt, "separator")
		value := rapid.StringMatching(`^[A-Za-z0-9]{3,12}$`).Draw(rt, "value")

		in := prefix + field + separator + value + suffix
		want := prefix + Replacement + suffix
		if got := Text(in); got != want {
			rt.Fatalf("Text(%q) = %q, want %q", in, got, want)
		}
	})
}

// Text leaves text alone when it holds no field name.
func TestTextKeepsTextWithoutFields(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		in := rapid.StringMatching(`^[0-9 XJ*@.+-]{0,60}$`).Draw(rt, "text")
		if got := Text(in); got != in {
			rt.Fatalf("Text(%q) = %q, want it unchanged", in, got)
		}
	})
}
